#!/usr/bin/env bash
# gVisor in this checkout's kind node (#136), so the workspace test runs its pod as production does; adapted from the
# trial of #132 (commit 3d350d8). make kind-up runs it in the kind tools container with CLUSTER set.
set -euo pipefail

# renovate: datasource=github-releases depName=google/gvisor
GVISOR=${GVISOR:-20260928.0}
WAIT=${WAIT:-300s}
BIN_MODE=0755
: "${CLUSTER:?set CLUSTER to the name of the kind cluster}"

if kubectl get runtimeclass gvisor >/dev/null 2>&1; then
    exit 0
fi
tmp=$(mktemp -d)
trap 'rm -rf "${tmp}"' EXIT
base="https://storage.googleapis.com/gvisor/releases/release/${GVISOR}/x86_64"
curl -fsSL -o "${tmp}/gvisor.tar.bz2" "${base}/gvisor.tar.bz2"
curl -fsSL -o "${tmp}/gvisor.tar.bz2.sha512" "${base}/gvisor.tar.bz2.sha512"
(cd "${tmp}" && sha512sum -c gvisor.tar.bz2.sha512 >/dev/null && tar -xjf gvisor.tar.bz2)
for node in $(kind get nodes --name "${CLUSTER}"); do
    for bin in runsc containerd-shim-runsc-v1; do
        docker cp "$(find "${tmp}" -name "${bin}" -type f | head -1)" "${node}:/usr/local/bin/${bin}"
        docker exec "${node}" chmod "${BIN_MODE}" "/usr/local/bin/${bin}"
    done
    # Newer releases split the sentry's helpers into gvisor-bin, which runsc looks for beside itself.
    if [ -d "${tmp}/gvisor-bin" ]; then
        docker cp "${tmp}/gvisor-bin" "${node}:/usr/local/bin/"
    fi
    # The runtime joins containerd's own configuration once; a restart of containerd picks it up.
    docker exec "${node}" sh -c 'grep -q runtimes.runsc /etc/containerd/config.toml || {
        printf "%s\n" "[plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runsc]" \
            "  runtime_type = \"io.containerd.runsc.v1\"" >> /etc/containerd/config.toml
        systemctl restart containerd
    }'
done
kubectl wait --for=condition=Ready nodes --all --timeout="${WAIT}"
kubectl apply -f - <<'YAML'
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: gvisor
handler: runsc
YAML
