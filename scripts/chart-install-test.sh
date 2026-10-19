#!/usr/bin/env bash
# Installs the chart into this checkout's kind cluster from signed images by digest, carries a run to its end in a
# sandbox pod (#29, #42), and upgrades it to admit only signed sandbox images; KEEP=1 leaves the release for a look.
set -euo pipefail

NS=rotor-chart
RELEASE=rotor
SANDBOXES="${NS}-sandboxes"
TAG=kind-test
RUN_WAIT=180
ADMISSION_WAIT=60
REGISTRY="${CLUSTER}-registry"
# The port the registry image serves on inside its container.
REGISTRY_PORT=5000
MANIFESTS="application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

say() { echo "== $*"; }

dump() {
    say "what the cluster saw"
    kubectl get pods -n "${NS}" -o wide || true
    kubectl get pods -n "${SANDBOXES}" -o wide || true
    kubectl get events -n "${NS}" --sort-by=.lastTimestamp | tail -30 || true
    kubectl get events -n "${SANDBOXES}" --sort-by=.lastTimestamp | tail -30 || true
    for c in api worker; do kubectl logs -n "${NS}" "deploy/${RELEASE}-${c}" --tail=60 || true; done
    kubectl logs -n cosign-system deploy/policy-controller-webhook --tail=40 2>/dev/null || true
}
trap 'status=$?; [ "$status" = 0 ] || dump' EXIT

say "a registry beside the cluster"
# The same registry by two names: this container pushes to it on the host's loopback, which Docker and cosign speak
# plain HTTP to, and the cluster pulls from its address on kind's network, which containerd is told is plain HTTP.
if [ -z "$(docker ps -q -f "name=^${REGISTRY}\$")" ]; then
    docker rm -f "${REGISTRY}" >/dev/null 2>&1 || true
    docker run -d --name "${REGISTRY}" --network kind -p "127.0.0.1::${REGISTRY_PORT}" "${REGISTRY_IMAGE}" >/dev/null
fi
pushed="localhost:$(docker port "${REGISTRY}" "${REGISTRY_PORT}/tcp" | head -1 | sed 's/.*://')"
pulled="$(docker inspect -f '{{(index .NetworkSettings.Networks "kind").IPAddress}}' "${REGISTRY}"):${REGISTRY_PORT}"
for node in $(kind get nodes --name "${CLUSTER}"); do
    docker exec "${node}" mkdir -p "/etc/containerd/certs.d/${pulled}"
    printf '[host."http://%s"]\n  capabilities = ["pull", "resolve"]\n' "${pulled}" |
        docker exec -i "${node}" cp /dev/stdin "/etc/containerd/certs.d/${pulled}/hosts.toml"
done

# Pushes a local image as name:tag and prints the digest the registry gave it.
push() { # image name tag
    docker tag "$1" "${pushed}/$2:$3"
    docker push -q "${pushed}/$2:$3" >/dev/null
    curl -sfI -H "Accept: ${MANIFESTS}" "http://${pushed}/v2/$2/manifests/$3" | tr -d '\r' |
        sed -n 's/^[Dd]ocker-[Cc]ontent-[Dd]igest: //p'
}

say "building, pushing, listing and signing the images"
docker build -q -f deploy/Dockerfile.backend -t "rotor-backend:${TAG}" . >/dev/null
docker build -q -f deploy/Dockerfile.web -t "rotor-web:${TAG}" . >/dev/null
backend=$(push "rotor-backend:${TAG}" rotor-backend "${TAG}")
web=$(push "rotor-web:${TAG}" rotor-web "${TAG}")
runner=$(push "${RUNNER_IMAGE}" rotor-runner "${TAG}")
images=("${pushed}/rotor-backend@${backend}" "${pushed}/rotor-web@${web}" "${pushed}/rotor-runner@${runner}")
keys=$(mktemp -d)
(cd "${keys}" && COSIGN_PASSWORD="" cosign generate-key-pair >/dev/null 2>&1)
export COSIGN_PASSWORD="" COSIGN_KEY="${keys}/cosign.key" COSIGN_PUB="${keys}/cosign.pub" REGISTRY_HTTP=1
for step in sbom sign attest verify; do scripts/supply.sh "${step}" "${images[@]}"; done

say "a dev OpenBao beside the release"
kubectl create namespace "${NS}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl apply -n "${NS}" -f scripts/kind/openbao.yaml >/dev/null
kubectl rollout status -n "${NS}" deploy/openbao --timeout=120s

say "installing the chart, every image by digest"
helm upgrade --install "${RELEASE}" deploy/charts/rotor -n "${NS}" \
    -f deploy/charts/rotor/values-trial.yaml -f scripts/kind/install-values.yaml \
    --set images.backend.repository="${pulled}/rotor-backend" --set images.backend.digest="${backend}" \
    --set images.web.repository="${pulled}/rotor-web" --set images.web.digest="${web}" \
    --set images.runner.repository="${pulled}/rotor-runner" --set images.runner.digest="${runner}" \
    --timeout 10m
for d in api worker web; do kubectl rollout status -n "${NS}" "deploy/${RELEASE}-${d}" --timeout=300s; done

psql() { kubectl exec -i -n "${NS}" "${RELEASE}-postgresql-0" -- psql -v ON_ERROR_STOP=1 -U postgres -d rotor -At "$@"; }

# The workflow the run carries. The node fails the run if it can open the runner's environment or memory, which the
# runner keeps from it (#89).
KIND_WORKFLOW='apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: kind}
limits: {maxSteps: 5, timeout: 2m}
entry: hello
nodes:
  - id: hello
    type: code
    output: {schema: {type: object}}
    source: |
      import os
      def run(state, ctx):
          for name in ("environ", "mem", "maps"):
              try:
                  open("/proc/%d/%s" % (os.getppid(), name), "rb").close()
              except PermissionError:
                  continue
              raise RuntimeError("the code node read the runner process " + name)
          return {"hello": "from a pod"}
'

# Asks the api, from inside its own pod, as the person whose session cookie is given.
api() { # session method path body
    kubectl exec -n "${NS}" "deploy/${RELEASE}-api" -- wget -qO- --header "Cookie: rotor_session=$1" \
        --header "Content-Type: application/json" --post-data "$4" "http://127.0.0.1:8080/api/v1$3"
}

# Starts a code node's run as a person whose session is made straight in the database, through the api, which alone
# can seal a run's inputs with its data key (#41), then waits until it succeeded and its pod is gone.
carry_run() {
    local run status="" session workflow
    session="kind-$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    psql >/dev/null <<SQL
WITH u AS (INSERT INTO app_user (email, name, role) VALUES ('kind-' || gen_random_uuid() || '@suite.test', 'Kind', 'member') RETURNING id)
INSERT INTO user_session (user_id, token_hash, expires_at)
SELECT id, sha256(convert_to('${session}', 'UTF8')), now() + interval '1 hour' FROM u;
SQL
    workflow=$(api "${session}" POST /workflows "$(jq -n --arg y "${KIND_WORKFLOW}" '{specYAML: $y, comment: "kind"}')" |
        jq -r .workflow.id)
    run=$(api "${session}" POST "/workflows/${workflow}/runs" '{"inputs": {}}' | jq -r .id)
    echo "run ${run}"
    for _ in $(seq "${RUN_WAIT}"); do
        status=$(psql -c "SELECT status FROM run WHERE id = '${run}'")
        case "${status}" in succeeded | failed | cancelled | expired) break ;; esac
        sleep 1
    done
    if [ "${status}" != succeeded ]; then
        echo "the run ended ${status}:" >&2
        psql -c "SELECT reason FROM run WHERE id = '${run}'" >&2
        psql -c "SELECT seq, type, node FROM run_event WHERE run_id = '${run}' ORDER BY seq" >&2
        exit 1
    fi
    echo "the run succeeded in its sandbox"
    for _ in $(seq 60); do
        [ -z "$(kubectl get pods -n "${SANDBOXES}" -l "rotor.dev/run=${run}" -o name)" ] && break
        sleep 1
    done
    if [ -n "$(kubectl get pods -n "${SANDBOXES}" -l "rotor.dev/run=${run}" -o name)" ]; then
        echo "the run's sandbox pod outlived it" >&2
        exit 1
    fi
    echo "its sandbox pod is gone"
}

say "a run, carried to its end in a sandbox pod"
carry_run

say "what a sandbox may reach"
kubectl apply -n "${SANDBOXES}" -f scripts/kind/egress-probe.yaml >/dev/null
kubectl wait -n "${SANDBOXES}" --for=condition=Ready pod/egress-probe --timeout=120s >/dev/null
# curl exits 6 when a name does not resolve, 7 when nothing answers and 28 when it times out, which a dropped packet
# makes; any other answer, an HTTP or TLS error included, means the probe reached the other side.
reaches() {
    local code=0
    kubectl exec -n "${SANDBOXES}" egress-probe -- curl -sk -o /dev/null --connect-timeout 5 --max-time 10 "$1" || code=$?
    [ "${code}" != 6 ] && [ "${code}" != 7 ] && [ "${code}" != 28 ]
}
allowed() { if reaches "$1"; then echo "   ok    reaches $2"; else echo "   FAIL  cannot reach $2" >&2; exit 1; fi; }
refused() { if reaches "$1"; then echo "   FAIL  reaches $2" >&2; exit 1; else echo "   ok    refused $2"; fi; }
allowed "https://${RELEASE}-api.${NS}.svc:9443" "the api's runner port"
allowed "http://openbao.${NS}.svc:8200/v1/sys/health" "OpenBao"
allowed "https://example.com" "an allowed host by name"
refused "https://example.net" "a host nobody allowed"
refused "http://169.254.169.254/latest/meta-data/" "cloud metadata"
refused "http://${RELEASE}-api.${NS}.svc:8080/readyz" "the api's HTTP port"
refused "http://${RELEASE}-web.${NS}.svc:8080/" "the web"
refused "https://kubernetes.default.svc" "the Kubernetes API"
kubectl delete pod -n "${SANDBOXES}" egress-probe --wait=false >/dev/null

say "policy-controller, which admits signed images only"
helm repo add sigstore "${POLICY_CONTROLLER_REPO}" --force-update >/dev/null
helm upgrade --install policy-controller sigstore/policy-controller --version "${POLICY_CONTROLLER_CHART}" \
    -n cosign-system --create-namespace --set leasescleanup.image.repository=alpine/kubectl \
    --set leasescleanup.image.version="${KUBECTL_VERSION}" --wait --timeout 10m >/dev/null

say "upgrading in place, to admit signed sandbox images only"
helm upgrade "${RELEASE}" deploy/charts/rotor -n "${NS}" --reuse-values --set sandbox.admission.requireSignature=true \
    --set-file sandbox.admission.publicKey="${COSIGN_PUB}" --wait --timeout 10m
echo "the upgrade came up"

say "a run in a sandbox pod whose image is signed"
carry_run

say "a sandbox image nobody signed"
unsigned_image=$(printf 'FROM %s\nLABEL rotor.dev/signed=no\n' "${RUNNER_IMAGE}" | docker build -q -)
unsigned=$(push "${unsigned_image}" rotor-runner unsigned)
probe() { sed "s|name: egress-probe|name: unsigned|; s|image: .*|image: ${pulled}/rotor-runner@${unsigned}|" scripts/kind/egress-probe.yaml; }
# The policy reaches the webhook a moment after the upgrade, so the first tries may still be admitted.
verdict=""
for _ in $(seq "${ADMISSION_WAIT}"); do
    if verdict=$(probe | kubectl apply -n "${SANDBOXES}" -f - 2>&1); then
        kubectl delete pod -n "${SANDBOXES}" unsigned --wait=false >/dev/null
        sleep 1
        continue
    fi
    break
done
for policy in signed sbom; do
    if ! grep -q "failed policy: ${RELEASE}-sandboxes-${policy}" <<<"${verdict}"; then
        echo "an unsigned sandbox image was not refused by the ${policy} policy: ${verdict}" >&2
        exit 1
    fi
    echo "   ok    refused by the ${policy} policy"
done

if [ "${KEEP:-}" != 1 ]; then
    say "removing the release"
    helm uninstall "${RELEASE}" -n "${NS}" --wait >/dev/null
    kubectl delete namespace "${NS}" "${SANDBOXES}" --ignore-not-found --wait=false >/dev/null
    kubectl delete clusterrolebinding rotor-chart-openbao-auth-delegator --ignore-not-found >/dev/null
fi
rm -rf "${keys}"
echo "PASS: the chart installs from signed images, runs a workflow in a sandbox pod, upgrades and refuses an unsigned one"
