#!/usr/bin/env bash
# The trial of #118, reverted once docs/decisions.md records it: OpenShell beside the KEEP=1 chart release.
# Usage, in the kind tools container: openshell-trial.sh install | rotor [runtime-class] | run
set -euo pipefail

GVISOR=20260928.0
AGENT_SANDBOX=v1.0.3
OPENSHELL=0.1.5
NS=rotor-chart
RELEASE=rotor
OS_NS=openshell
OS_SANDBOXES=openshell-sandboxes
TAG=openshell-trial
RUN_WAIT=300
REGISTRY="${CLUSTER}-registry"
REGISTRY_PORT=5000
MANIFESTS="application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"
say() { echo "== $*"; }
pushed="localhost:$(docker port "${REGISTRY}" "${REGISTRY_PORT}/tcp" | head -1 | sed 's/.*://')"
pulled="$(docker inspect -f '{{(index .NetworkSettings.Networks "kind").IPAddress}}' "${REGISTRY}"):${REGISTRY_PORT}"
psql() { kubectl exec -i -n "${NS}" "${RELEASE}-postgresql-0" -- psql -v ON_ERROR_STOP=1 -U postgres -d rotor -At "$@"; }
api() {
    kubectl exec -n "${NS}" "deploy/${RELEASE}-api" -- wget -qO- --header "Cookie: rotor_session=$1" \
        --header "Content-Type: application/json" --post-data "$4" "http://127.0.0.1:8080/api/v1$3"
}

sql() { psql -c "$1"; }

# The proxy settings of the newest sandbox's processes, as OpenShell launched them.
penv() {
    p=$(kubectl get pods -n "${OS_SANDBOXES}" --sort-by=.metadata.creationTimestamp -o name | grep "default--" | tail -1)
    kubectl exec -n "${OS_SANDBOXES}" "${p}" -c agent -- /opt/venv/bin/python -c '
import os
for pid in os.listdir("/proc"):
    if not pid.isdigit():
        continue
    try:
        cmd = open("/proc/%s/cmdline" % pid, "rb").read().replace(b"\0", b" ")[:80]
        env = open("/proc/%s/environ" % pid, "rb").read().split(b"\0")
    except OSError as err:
        print(pid, "unreadable", err)
        continue
    print(pid, cmd, [e for e in env if b"proxy" in e.lower() or b"PROXY" in e])
'
}

# A service that logs the Authorization header each request carries, for the rotation probe.
echo_service() {
    kubectl apply -n "${OS_SANDBOXES}" -f - <<YAML
apiVersion: v1
kind: Pod
metadata:
  name: echo
  labels: {app: echo}
spec:
  securityContext: {runAsNonRoot: true, runAsUser: 65532, seccompProfile: {type: RuntimeDefault}}
  containers:
    - name: echo
      image: ${pulled}/rotor-runner:kind-test
      securityContext: {allowPrivilegeEscalation: false, capabilities: {drop: [ALL]}}
      command:
        - /opt/venv/bin/python
        - -c
        - |
          import http.server
          class H(http.server.BaseHTTPRequestHandler):
              def do_GET(self):
                  print("who=%s auth=%s" % (self.headers.get("X-Who"), self.headers.get("Authorization")), flush=True)
                  self.send_response(200)
                  self.end_headers()
          http.server.HTTPServer(("", 8080), H).serve_forever()
---
apiVersion: v1
kind: Service
metadata:
  name: echo
spec:
  selector: {app: echo}
  ports: [{port: 8080}]
YAML
    kubectl wait -n "${OS_SANDBOXES}" --for=condition=Ready pod/echo --timeout=120s
}

echo_logs() { kubectl logs -n "${OS_SANDBOXES}" echo --tail="${1:-40}"; }

# The gateway's database, copied out to look for a credential's value in it.
gateway_db() {
    kubectl exec -n "${OS_NS}" openshell-0 -- sh -c 'cat /var/openshell/openshell.db /var/openshell/openshell.db-wal 2>/dev/null' \
        > .cache/kind/openshell-trial.db
    ls -l .cache/kind/openshell-trial.db
}

# Runs a command in the gateway's pod, such as the openshell CLI if the image has it.
gw() { kubectl exec -i -n "${OS_NS}" openshell-0 -- "$@"; }

# The gateway on the host's port 18080, for cmd/openshell-trial; runs until stopped.
forward() { kubectl port-forward -n "${OS_NS}" svc/openshell 18080:8080; }

install() {
    say "gVisor ${GVISOR} in the kind node"
    tmp=$(mktemp -d)
    base="https://storage.googleapis.com/gvisor/releases/release/${GVISOR}/x86_64"
    curl -fsSL -o "${tmp}/gvisor.tar.bz2" "${base}/gvisor.tar.bz2"
    curl -fsSL -o "${tmp}/gvisor.tar.bz2.sha512" "${base}/gvisor.tar.bz2.sha512"
    (cd "${tmp}" && sha512sum -c gvisor.tar.bz2.sha512 && tar -xjf gvisor.tar.bz2 && ls -R | head -20)
    for node in $(kind get nodes --name "${CLUSTER}"); do
        for bin in runsc containerd-shim-runsc-v1; do
            docker cp "$(find "${tmp}" -name "${bin}" -type f | head -1)" "${node}:/usr/local/bin/${bin}"
            docker exec "${node}" chmod 0755 "/usr/local/bin/${bin}"
        done
        docker cp "$(find "${tmp}" -name gvisor-bin -type d | head -1)" "${node}:/usr/local/bin/"
        docker exec "${node}" sh -c 'grep -q runtimes.runsc /etc/containerd/config.toml || printf "%s\n" \
            "[plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runsc]" \
            "  runtime_type = \"io.containerd.runsc.v1\"" >> /etc/containerd/config.toml; systemctl restart containerd'
    done
    kubectl wait --for=condition=Ready nodes --all --timeout=120s
    kubectl apply -f - <<'YAML'
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: gvisor
  labels:
    openshell.ai/sandbox-attachable: "true"
    openshell.ai/sandbox-attachable-workspace: default
handler: runsc
YAML

    say "Agent Sandbox ${AGENT_SANDBOX}"
    kubectl apply -f "https://github.com/kubernetes-sigs/agent-sandbox/releases/download/${AGENT_SANDBOX}/sandbox.yaml"
    kubectl wait --for=condition=Established crd/sandboxes.agents.x-k8s.io --timeout=120s
    kubectl -n agent-sandbox-system rollout status deployment/agent-sandbox-controller --timeout=300s

    say "OpenShell ${OPENSHELL}"
    kubectl create namespace "${OS_SANDBOXES}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
    kubectl label namespace "${OS_SANDBOXES}" pod-security.kubernetes.io/enforce=restricted --overwrite >/dev/null
    helm upgrade --install openshell oci://ghcr.io/nvidia/openshell/helm-chart --version "${OPENSHELL}" \
        -n "${OS_NS}" --create-namespace -f scripts/kind/openshell-values.yaml --wait --timeout 10m
    kubectl -n "${OS_NS}" get pods -o wide
}

# A plain pod of the runner image under gVisor, before OpenShell is involved.
gvisor() {
    kubectl delete pod gvisor-check -n "${OS_SANDBOXES}" --ignore-not-found >/dev/null
    kubectl run gvisor-check -n "${OS_SANDBOXES}" --restart=Never --image="${pulled}/rotor-runner:kind-test" \
        --overrides='{"spec":{"runtimeClassName":"gvisor","securityContext":{"runAsNonRoot":true,"runAsUser":65532,"seccompProfile":{"type":"RuntimeDefault"}},"containers":[{"name":"gvisor-check","image":"'"${pulled}"'/rotor-runner:kind-test","command":["/opt/venv/bin/python","-c","import platform;print(platform.release(), platform.version())"],"securityContext":{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]}}}]}}'
    kubectl wait -n "${OS_SANDBOXES}" --for=jsonpath='{.status.phase}'=Succeeded pod/gvisor-check --timeout=120s || true
    kubectl get pod gvisor-check -n "${OS_SANDBOXES}" -o wide
    kubectl logs -n "${OS_SANDBOXES}" gvisor-check || true
}

rotor() {
    say "Rotor's backend with the trial backend, and the worker on OpenShell"
    docker build -q -f deploy/Dockerfile.backend -t "rotor-backend:${TAG}" . >/dev/null
    docker tag "rotor-backend:${TAG}" "${pushed}/rotor-backend:${TAG}"
    docker push -q "${pushed}/rotor-backend:${TAG}" >/dev/null
    digest=$(curl -sfI -H "Accept: ${MANIFESTS}" "http://${pushed}/v2/rotor-backend/manifests/${TAG}" | tr -d '\r' |
        sed -n 's/^[Dd]ocker-[Cc]ontent-[Dd]igest: //p')
    docker tag "${RUNNER_IMAGE}" "${pushed}/rotor-runner:${TAG}"
    docker push -q "${pushed}/rotor-runner:${TAG}" >/dev/null
    runner=$(curl -sfI -H "Accept: ${MANIFESTS}" "http://${pushed}/v2/rotor-runner/manifests/${TAG}" | tr -d '\r' |
        sed -n 's/^[Dd]ocker-[Cc]ontent-[Dd]igest: //p')
    helm upgrade "${RELEASE}" deploy/charts/rotor -n "${NS}" --reuse-values \
        --set images.backend.repository="${pulled}/rotor-backend" --set images.backend.digest="${digest}" \
        --set images.runner.repository="${pulled}/rotor-runner" --set images.runner.digest="${runner}" \
        --set sandbox.admission.requireSignature=false \
        --set sandbox.backend=openshell --set sandbox.openshell.addr="http://openshell.${OS_NS}.svc.cluster.local:8080" \
        --set sandbox.runtimeClass="${1-gvisor}" --timeout 10m
    for d in api worker; do kubectl rollout status -n "${NS}" "deploy/${RELEASE}-${d}" --timeout=300s; done
}

WORKFLOW='apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: openshell-trial}
limits: {maxSteps: 5, timeout: 4m}
entry: hello
nodes:
  - id: hello
    type: code
    output: {schema: {type: object}}
    source: |
      import os, socket
      def run(state, ctx):
          read = []
          for name in ("environ", "mem", "maps"):
              try:
                  open("/proc/%d/%s" % (os.getppid(), name), "rb").close()
                  read.append(name)
              except OSError:
                  continue
          host, port = os.environ.get("ROTOR_CONTROL_PLANE_ADDR", "rotor-api.rotor-chart.svc:9443").rsplit(":", 1)
          try:
              socket.create_connection((host, int(port)), timeout=5).close()
              reach = "the code node reached the runner port"
          except OSError as err:
              reach = "the code node could not reach the runner port: %s" % err
          return {"hello": "from openshell", "reach": reach, "read_of_runner": read}
'

run() {
    say "a run on the OpenShell backend"
    session="trial-$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    psql >/dev/null <<SQL
WITH u AS (INSERT INTO app_user (email, name, role) VALUES ('trial-' || gen_random_uuid() || '@suite.test', 'Trial', 'member') RETURNING id)
INSERT INTO user_session (user_id, token_hash, expires_at)
SELECT id, sha256(convert_to('${session}', 'UTF8')), now() + interval '1 hour' FROM u;
SQL
    workflow=$(api "${session}" POST /workflows "$(jq -n --arg y "${WORKFLOW}" '{specYAML: $y, comment: "trial"}')" |
        jq -r .workflow.id)
    run=$(api "${session}" POST "/workflows/${workflow}/runs" '{"inputs": {}}' | jq -r .id)
    echo "run ${run}"
    status=""
    for _ in $(seq "${RUN_WAIT}"); do
        status=$(psql -c "SELECT status FROM run WHERE id = '${run}'")
        case "${status}" in succeeded | failed | cancelled | expired) break ;; esac
        sleep 1
    done
    echo "the run ended ${status}"
    psql -c "SELECT status, reason FROM run WHERE id = '${run}'"
    psql -c "SELECT seq, type, node FROM run_event WHERE run_id = '${run}' ORDER BY seq"
    psql -c "SELECT (SELECT min(at) FROM run_event WHERE run_id = r.id) - r.created_at FROM run r WHERE id = '${run}'"
    kubectl exec -n "${NS}" "deploy/${RELEASE}-api" -- wget -qO- --header "Cookie: rotor_session=${session}" \
        "http://127.0.0.1:8080/api/v1/runs/${run}" | jq -c '.output // .run.output // .'
    echo "${run}" > .cache/kind/openshell-trial-run
}

"$@"
