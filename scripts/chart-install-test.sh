#!/usr/bin/env bash
# Installs the chart into this checkout's kind cluster beside a dev OpenBao and carries a run to its end in a sandbox
# pod (#29); make test-chart-install runs it in the kind tools container, KEEP=1 leaves the release for a look.
set -euo pipefail

NS=rotor-chart
RELEASE=rotor
SANDBOXES="${NS}-sandboxes"
TAG=kind-test
RUN_WAIT=180

say() { echo "== $*"; }

dump() {
    say "what the cluster saw"
    kubectl get pods -n "${NS}" -o wide || true
    kubectl get pods -n "${SANDBOXES}" -o wide || true
    kubectl get events -n "${NS}" --sort-by=.lastTimestamp | tail -30 || true
    for c in api worker; do kubectl logs -n "${NS}" "deploy/${RELEASE}-${c}" --tail=60 || true; done
}
trap 'status=$?; [ "$status" = 0 ] || dump' EXIT

say "building the backend and web images"
docker build -q -f deploy/Dockerfile.backend -t "rotor-backend:${TAG}" . >/dev/null
docker build -q -f deploy/Dockerfile.web -t "rotor-web:${TAG}" . >/dev/null
for image in "rotor-backend:${TAG}" "rotor-web:${TAG}" "${RUNNER_IMAGE}"; do
    kind load docker-image "${image}" --name "${CLUSTER}" >/dev/null
done

say "a dev OpenBao beside the release"
kubectl create namespace "${NS}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl apply -n "${NS}" -f scripts/kind/openbao.yaml >/dev/null
kubectl rollout status -n "${NS}" deploy/openbao --timeout=120s

say "installing the chart"
helm upgrade --install "${RELEASE}" deploy/charts/rotor -n "${NS}" \
    -f deploy/charts/rotor/values-trial.yaml -f scripts/kind/install-values.yaml \
    --set images.backend.repository=rotor-backend --set images.backend.tag="${TAG}" \
    --set images.web.repository=rotor-web --set images.web.tag="${TAG}" \
    --set images.runner.repository="${RUNNER_IMAGE%:*}" --set images.runner.tag="${RUNNER_IMAGE##*:}" \
    --timeout 10m
for d in api worker web; do kubectl rollout status -n "${NS}" "deploy/${RELEASE}-${d}" --timeout=300s; done

say "a run, carried to its end in a sandbox pod"
psql() { kubectl exec -i -n "${NS}" "${RELEASE}-postgresql-0" -- psql -v ON_ERROR_STOP=1 -U postgres -d rotor -At "$@"; }
run=$(psql <<'SQL'
WITH u AS (INSERT INTO app_user (email, name, role) VALUES ('kind-' || gen_random_uuid() || '@suite.test', 'Kind', 'member') RETURNING id),
     w AS (INSERT INTO workflow (name, owner_id) SELECT 'kind', id FROM u RETURNING id, owner_id),
     v AS (INSERT INTO workflow_version (workflow_id, number, spec_yaml, spec, hash)
           SELECT id, 1, E'apiVersion: rotor.dev/v1\nkind: Workflow\nmetadata: {name: kind}\nlimits: {maxSteps: 5, timeout: 2m}\nentry: hello\nnodes:\n  - id: hello\n    type: code\n    output: {schema: {type: object}}\n    source: |\n      def run(state, ctx):\n          return {"hello": "from a pod"}\n',
                  '{"limits": {"timeout": "2m"}}', 'kind' FROM w RETURNING workflow_id)
INSERT INTO run (workflow_id, version, user_id) SELECT v.workflow_id, 1, w.owner_id FROM v JOIN w ON w.id = v.workflow_id RETURNING id;
SQL
)
run=$(echo "${run}" | head -1)
echo "run ${run}"
status=""
for _ in $(seq "${RUN_WAIT}"); do
    status=$(psql -c "SELECT status FROM run WHERE id = '${run}'")
    case "${status}" in succeeded | failed | cancelled | expired) break ;; esac
    sleep 1
done
if [ "${status}" != succeeded ]; then
    echo "the run ended ${status}:" >&2
    psql -c "SELECT seq, type, node, payload FROM run_event WHERE run_id = '${run}' ORDER BY seq" >&2
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

say "upgrading in place"
helm upgrade "${RELEASE}" deploy/charts/rotor -n "${NS}" --reuse-values --wait --timeout 10m
echo "the upgrade came up"

if [ "${KEEP:-}" != 1 ]; then
    say "removing the release"
    helm uninstall "${RELEASE}" -n "${NS}" --wait >/dev/null
    kubectl delete namespace "${NS}" "${SANDBOXES}" --ignore-not-found --wait=false >/dev/null
    kubectl delete clusterrolebinding rotor-chart-openbao-auth-delegator --ignore-not-found >/dev/null
fi
echo "PASS: the chart installs, runs a workflow in a sandbox pod and upgrades"
