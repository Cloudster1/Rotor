#!/usr/bin/env bash
# The trial of #132, reverted once docs/decisions.md records it: a workspace pod under gVisor on the kind cluster.
# Usage, in the kind tools container: workspace-trial.sh gvisor | images | forge | pod [Memory|disk] | step ... | probe
set -euo pipefail

GVISOR=20260928.0
GOLANG=golang:1.26.8-alpine@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c
FORGE_IMAGE=gitea/gitea:1.24-rootless@sha256:a664a64d7740ef0db74f090f567040d0e5b34592473aad08ed5e296ccbe00eab
AGENT=rotor-workspace-agent:trial
RUNNER=rotor-runner-trial:trial
NS=rotor-sandboxes
FORGE_NS=trial-forge
FORGE_USER=trial
FORGE_PASSWORD=trial-password-132
FORGE_PORT=3000
FORWARD_PORT=13132
DOCKER_FORGE_PORT=13133
POD=workspace-trial
WAIT=300s
say() { echo "== $*"; }

gvisor() {
    say "gVisor ${GVISOR} in the kind node"
    tmp=$(mktemp -d)
    base="https://storage.googleapis.com/gvisor/releases/release/${GVISOR}/x86_64"
    curl -fsSL -o "${tmp}/gvisor.tar.bz2" "${base}/gvisor.tar.bz2"
    curl -fsSL -o "${tmp}/gvisor.tar.bz2.sha512" "${base}/gvisor.tar.bz2.sha512"
    (cd "${tmp}" && sha512sum -c gvisor.tar.bz2.sha512 && tar -xjf gvisor.tar.bz2)
    for node in $(kind get nodes --name "${CLUSTER}"); do
        for bin in runsc containerd-shim-runsc-v1; do
            docker cp "$(find "${tmp}" -name "${bin}" -type f | head -1)" "${node}:/usr/local/bin/${bin}"
            docker exec "${node}" chmod 0755 "/usr/local/bin/${bin}"
        done
        if found=$(find "${tmp}" -name gvisor-bin -type d | head -1) && [ -n "${found}" ]; then
            docker cp "${found}" "${node}:/usr/local/bin/"
        fi
        docker exec "${node}" sh -c 'grep -q runtimes.runsc /etc/containerd/config.toml || printf "%s\n" \
            "[plugins.\"io.containerd.grpc.v1.cri\".containerd.runtimes.runsc]" \
            "  runtime_type = \"io.containerd.runsc.v1\"" >> /etc/containerd/config.toml
            # The pod mount hints reach runsc only when containerd passes the pod annotations on to it.
            grep -q pod_annotations /etc/containerd/config.toml || sed -i "/runtime_type = \"io.containerd.runsc.v1\"/a\  pod_annotations = [\"dev.gvisor.*\"]" /etc/containerd/config.toml
            systemctl restart containerd'
    done
    kubectl wait --for=condition=Ready nodes --all --timeout="${WAIT}"
    kubectl apply -f - <<'YAML'
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: gvisor
handler: runsc
YAML
}

images() {
    say "the agent and the runner with dulwich, and the toolchain and forge images, into the node"
    docker build -q --target agent -t "${AGENT}" scripts/kind/workspace-trial >/dev/null
    docker build -q --target runner --build-arg RUNNER_IMAGE="${RUNNER_IMAGE}" -t "${RUNNER}" scripts/kind/workspace-trial >/dev/null
    for image in "${AGENT}" "${RUNNER}"; do
        kind load docker-image "${image}" --name "${CLUSTER}"
    done
    docker image ls --format '{{.Repository}}:{{.Tag}} {{.Size}}' | grep -E "workspace-agent|runner-trial"
}

api() {
    local method=$1 path=$2 body=${3:-}
    curl -fsS -u "${FORGE_USER}:${FORGE_PASSWORD}" -X "${method}" -H 'Content-Type: application/json' \
        ${body:+--data "${body}"} "http://127.0.0.1:${FORWARD_PORT}/api/v1${path}"
}

# A repository of the forge made from a directory of scripts/kind/workspace-trial.
seed() {
    api POST /user/repos '{"name":"'"$1"'","auto_init":true,"default_branch":"main"}' >/dev/null
    files=$(for f in $(ls "scripts/kind/workspace-trial/$1"); do
        jq -n --arg p "${f}" --arg c "$(base64 -w0 "scripts/kind/workspace-trial/$1/${f}")" \
            '{operation: "create", path: $p, content: $c}'
    done | jq -s '{files: ., message: "add the module", branch: "main"}')
    api POST "/repos/${FORGE_USER}/$1/contents" "${files}" | jq -r .commit.sha
}

# Gitea, standing in for Armature's git proxy (Cloudster1/Armature#332), with the small module and Rotor itself.
forge() {
    say "Gitea in ${FORGE_NS}"
    kubectl delete pod -n "${FORGE_NS}" forge --ignore-not-found --wait >/dev/null 2>&1 || true
    kubectl create namespace "${FORGE_NS}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
    kubectl apply -n "${FORGE_NS}" -f - <<YAML
apiVersion: v1
kind: Pod
metadata:
  name: forge
  labels: {app: forge}
spec:
  securityContext: {runAsNonRoot: true, runAsUser: 1000, runAsGroup: 1000, fsGroup: 1000, seccompProfile: {type: RuntimeDefault}}
  containers:
    - name: forge
      image: ${FORGE_IMAGE}
      imagePullPolicy: IfNotPresent
      securityContext: {allowPrivilegeEscalation: false, capabilities: {drop: [ALL]}}
      env:
        - {name: GITEA__security__INSTALL_LOCK, value: "true"}
        - {name: GITEA__database__DB_TYPE, value: sqlite3}
        - {name: GITEA__server__ROOT_URL, value: "http://forge.${FORGE_NS}.svc.cluster.local:${FORGE_PORT}/"}
        - {name: GITEA__service__DISABLE_REGISTRATION, value: "true"}
        - {name: GITEA__migrations__ALLOWED_DOMAINS, value: github.com}
      resources: {limits: {memory: 512Mi}}
      readinessProbe: {httpGet: {path: /api/healthz, port: ${FORGE_PORT}}, periodSeconds: 2}
      volumeMounts:
        - {name: data, mountPath: /var/lib/gitea}
        - {name: config, mountPath: /etc/gitea}
  volumes:
    - {name: data, emptyDir: {}}
    - {name: config, emptyDir: {}}
---
apiVersion: v1
kind: Service
metadata:
  name: forge
spec:
  selector: {app: forge}
  ports: [{port: ${FORGE_PORT}}]
YAML
    kubectl wait -n "${FORGE_NS}" --for=condition=Ready pod/forge --timeout="${WAIT}"
    for _ in $(seq 60); do
        kubectl exec -n "${FORGE_NS}" forge -- gitea admin user create --admin --username "${FORGE_USER}" \
            --password "${FORGE_PASSWORD}" --email trial@rotor.invalid --must-change-password=false && break
        sleep 2
    done
    kubectl port-forward -n "${FORGE_NS}" svc/forge "${FORWARD_PORT}:${FORGE_PORT}" >/dev/null &
    forward=$!
    trap 'kill ${forward}' EXIT
    sleep 3
    say "the small module, with a test"
    seed hello
    seed plain
    say "Rotor, migrated from GitHub with its history"
    time api POST /repos/migrate '{"clone_addr":"https://github.com/Cloudster1/Rotor.git","repo_name":"rotor","repo_owner":"'"${FORGE_USER}"'","mirror":false}' | jq -r .size
    say "a token for the pod, as the run's Armature token would be"
    token=$(kubectl exec -n "${FORGE_NS}" forge -- gitea admin user generate-access-token --username "${FORGE_USER}" \
        --token-name "pod-$(date +%s)" --scopes write:repository --raw)
    kubectl create secret generic -n "${NS}" "${POD}" --from-literal=FORGE_URL="http://${FORGE_USER}:${token}@forge.${FORGE_NS}.svc.cluster.local:${FORGE_PORT}/${FORGE_USER}" \
        --dry-run=client -o yaml | kubectl apply -f -
}

# Another public repository from GitHub into the forge, for clone sizes: migrate <owner/repo> <name>.
migrate() {
    kubectl port-forward -n "${FORGE_NS}" svc/forge "${FORWARD_PORT}:${FORGE_PORT}" >/dev/null &
    forward=$!
    trap 'kill ${forward}' EXIT
    sleep 3
    time api POST /repos/migrate '{"clone_addr":"https://github.com/'"$1"'.git","repo_name":"'"$2"'","repo_owner":"'"${FORGE_USER}"'","mirror":false}' | jq -r .size
    api GET "/repos/${FORGE_USER}/$2" | jq -c '{size, default_branch}'
}

# The egress the decision names: DNS, the git host, the Go module proxy and its checksum database, the model provider.
egress() {
    kubectl apply -n "${NS}" -f - <<YAML
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: ${POD}
spec:
  podSelector: {matchLabels: {rotor.dev/workspace-trial: "1"}}
  policyTypes: [Ingress, Egress]
  ingress: []
  egress:
    - to: [{namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: kube-system}}, podSelector: {matchLabels: {k8s-app: kube-dns}}}]
      ports: [{protocol: UDP, port: 53}, {protocol: TCP, port: 53}]
    - to: [{namespaceSelector: {matchLabels: {kubernetes.io/metadata.name: ${FORGE_NS}}}, podSelector: {matchLabels: {app: forge}}}]
      ports: [{protocol: TCP, port: ${FORGE_PORT}}]
---
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: ${POD}-hosts
spec:
  endpointSelector: {matchLabels: {rotor.dev/workspace-trial: "1"}}
  egress:
    - toEndpoints: [{matchLabels: {k8s:io.kubernetes.pod.namespace: kube-system, k8s:k8s-app: kube-dns}}]
      toPorts: [{ports: [{port: "53", protocol: ANY}], rules: {dns: [{matchPattern: "*"}]}}]
    - toFQDNs: [{matchName: proxy.golang.org}, {matchName: sum.golang.org}, {matchName: storage.googleapis.com}, {matchName: api.anthropic.com}]
      toPorts: [{ports: [{port: "443", protocol: TCP}]}]
YAML
}

# The workspace pod: the agent installed by an init container, the runner and the toolchain sharing the workspace.
pod() {
    local medium=${1:-Memory} hints=${2:-}
    egress
    kubectl delete pod -n "${NS}" "${POD}" --ignore-not-found --wait >/dev/null
    [ "${medium}" = disk ] && medium=""
    # gVisor's pod mount hints: a volume of the sandbox's own memory, shared by its containers, instead of a host
    # directory over the gofer, which each container sees apart.
    annotations="{}"
    if [ -n "${hints}" ]; then
        annotations="{$(for v in ${hints}; do printf '"dev.gvisor.spec.mount.%s.share": pod, "dev.gvisor.spec.mount.%s.type": tmpfs, "dev.gvisor.spec.mount.%s.options": "rw,rprivate", ' "${v}" "${v}" "${v}"; done)}"
    fi
    start=$(date +%s.%N)
    kubectl apply -n "${NS}" -f - <<YAML
apiVersion: v1
kind: Pod
metadata:
  name: ${POD}
  labels: {rotor.dev/workspace-trial: "1"}
  annotations: ${annotations}
spec:
  runtimeClassName: gvisor
  restartPolicy: Never
  automountServiceAccountToken: false
  enableServiceLinks: false
  serviceAccountName: rotor-sandbox
  securityContext: {runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532, seccompProfile: {type: RuntimeDefault}}
  initContainers:
    - name: agent
      image: ${AGENT}
      imagePullPolicy: IfNotPresent
      args: [install, /rotor/bin/agent]
      securityContext: &hardened {readOnlyRootFilesystem: true, allowPrivilegeEscalation: false, privileged: false, capabilities: {drop: [ALL]}}
      volumeMounts: [{name: bin, mountPath: /rotor/bin}]
  containers:
    - name: runner
      image: ${RUNNER}
      imagePullPolicy: IfNotPresent
      command: [/opt/venv/bin/python, -c, "import time; time.sleep(86400)"]
      envFrom: [{secretRef: {name: ${POD}}}]
      securityContext: *hardened
      resources: {limits: &runner {cpu: "1", memory: 1536Mi}, requests: *runner}
      volumeMounts:
        - {name: tmp, mountPath: /tmp}
        - {name: private, mountPath: /private}
        - {name: workspace, mountPath: /workspace}
        - {name: control, mountPath: /control}
    - name: toolchain
      image: ${GOLANG}
      imagePullPolicy: IfNotPresent
      command: [/rotor/bin/agent, serve, /control/agent.sock]
      workingDir: /workspace
      env:
        - {name: HOME, value: /cache}
        - {name: TMPDIR, value: /cache}
        - {name: GOCACHE, value: /cache/go-build}
        - {name: GOMODCACHE, value: /cache/mod}
        - {name: GOTOOLCHAIN, value: local}
        - {name: GOFLAGS, value: -mod=readonly}
        - {name: GOPROXY, value: https://proxy.golang.org}
      securityContext: *hardened
      resources: {limits: &toolchain {cpu: "2", memory: 3Gi}, requests: *toolchain}
      volumeMounts:
        - {name: bin, mountPath: /rotor/bin, readOnly: true}
        - {name: control, mountPath: /control}
        - {name: workspace, mountPath: /workspace}
        - {name: cache, mountPath: /cache}
  volumes:
    - {name: bin, emptyDir: {medium: Memory, sizeLimit: 16Mi}}
    - {name: control, emptyDir: {medium: Memory, sizeLimit: 1Mi}}
    - {name: tmp, emptyDir: {medium: Memory, sizeLimit: 64Mi}}
    - {name: private, emptyDir: {medium: "${medium}", sizeLimit: 1Gi}}
    - {name: workspace, emptyDir: {medium: "${medium}", sizeLimit: 1Gi}}
    - {name: cache, emptyDir: {sizeLimit: 4Gi}}
YAML
    kubectl wait -n "${NS}" --for=condition=Ready "pod/${POD}" --timeout="${WAIT}"
    echo "ready after $(awk "BEGIN{print $(date +%s.%N) - ${start}}") s"
    kubectl get pod -n "${NS}" "${POD}" -o json | jq -r '.metadata.creationTimestamp as $c |
        (.status.initContainerStatuses + .status.containerStatuses)[] | "\(.name) \(.state | to_entries[0].value.startedAt // .state) created \($c)"'
}

# One call of the driver in the runner container: clone, exec, edit, read or push.
step() { kubectl exec -n "${NS}" "${POD}" -c runner -- /opt/venv/bin/python /opt/trial/driver.py "$@"; }

# Runs a command in a container of the pod, for what the decision reports about it.
inside() { local c=$1; shift; kubectl exec -n "${NS}" "${POD}" -c "${c}" -- "$@"; }

# Where each container of the pod can connect: the policy is the pod's, so both get the same answer.
probe() {
    for url in https://proxy.golang.org/github.com/google/go-cmp/@v/list https://sum.golang.org/lookup/github.com/google/go-cmp@v0.7.0 \
        https://api.anthropic.com/ https://example.com/ https://github.com/ https://registry.npmjs.org/ \
        "http://forge.${FORGE_NS}.svc.cluster.local:${FORGE_PORT}/" http://kubernetes.default.svc/; do
        tool=$(step exec /workspace 15 -- wget -T 8 -q -O /dev/null "${url}" 2>&1 | sed -n 1p | jq -r '"exit \(.exitCode)"')
        runner=$(kubectl exec -n "${NS}" "${POD}" -c runner -- /opt/venv/bin/python -c '
import sys, urllib.request, urllib.error
try:
    print(urllib.request.urlopen(sys.argv[1], timeout=8).status)
except urllib.error.HTTPError as err:
    print(err.code)
except Exception as err:
    print(type(err).__name__)' "${url}")
        echo "${url}: toolchain ${tool}, runner ${runner}"
    done
}

# Whether the agent could come as an image volume instead of an init container's copy.
imagevolume() {
    kubectl delete pod -n "${NS}" image-volume --ignore-not-found --wait >/dev/null
    kubectl apply -n "${NS}" -f - <<YAML
apiVersion: v1
kind: Pod
metadata:
  name: image-volume
spec:
  runtimeClassName: gvisor
  restartPolicy: Never
  automountServiceAccountToken: false
  securityContext: {runAsNonRoot: true, runAsUser: 65532, runAsGroup: 65532, seccompProfile: {type: RuntimeDefault}}
  containers:
    - name: toolchain
      image: ${GOLANG}
      command: [/rotor/bin/agent]
      securityContext: {readOnlyRootFilesystem: true, allowPrivilegeEscalation: false, capabilities: {drop: [ALL]}}
      volumeMounts: [{name: bin, mountPath: /rotor/bin}]
  volumes:
    - {name: bin, image: {reference: "${AGENT}", pullPolicy: IfNotPresent}}
YAML
    sleep 10
    kubectl get pod -n "${NS}" image-volume -o jsonpath='{.spec.volumes[0]}{"\n"}{.status.phase}{"\n"}'
    kubectl logs -n "${NS}" image-volume || true
    kubectl delete pod -n "${NS}" image-volume --wait=false
}

# Whether the runner reaches a listener in the toolchain container over the pod's loopback, hints or not.
loopback() {
    kubectl exec -n "${NS}" "${POD}" -c toolchain -- timeout 20 nc -l -p 9999 &
    sleep 4
    kubectl exec -n "${NS}" "${POD}" -c runner -- /opt/venv/bin/python -c '
import os, socket
s = socket.create_connection(("127.0.0.1", 9999), timeout=5)
s.sendall(b"hello over the loopback\n")
s.close()
print("sent; /control holds", os.listdir("/control"))'
    wait
}

# The same shape on Docker, as the docker backend would make it for the gate: an internal network, the toolchain in
# the runner's network namespace, the agent as an image mount, the workspace a tmpfs volume both containers mount.
D=rotor-trial-132
docker_up() {
    docker_down >/dev/null 2>&1 || true
    docker network create --internal "${D}" >/dev/null
    docker run -d --name "${D}-forge" -p "127.0.0.1:${DOCKER_FORGE_PORT}:${FORGE_PORT}" \
        -e GITEA__security__INSTALL_LOCK=true -e GITEA__database__DB_TYPE=sqlite3 \
        -e GITEA__server__ROOT_URL="http://forge:${FORGE_PORT}/" "${FORGE_IMAGE}" >/dev/null
    docker network connect --alias forge "${D}" "${D}-forge"
    for _ in $(seq 60); do
        docker exec "${D}-forge" gitea admin user create --admin --username "${FORGE_USER}" --password "${FORGE_PASSWORD}" \
            --email trial@rotor.invalid --must-change-password=false >/dev/null 2>&1 && break
        sleep 2
    done
    until curl -fs "http://127.0.0.1:${DOCKER_FORGE_PORT}/api/healthz" >/dev/null; do sleep 1; done
    FORWARD_PORT=${DOCKER_FORGE_PORT} seed plain
    FORWARD_PORT=${DOCKER_FORGE_PORT} seed hello
    token=$(docker exec "${D}-forge" gitea admin user generate-access-token --username "${FORGE_USER}" \
        --token-name docker --scopes write:repository --raw)
    for v in workspace control; do
        docker volume create --driver local --opt type=tmpfs --opt device=tmpfs \
            --opt o=size=1g,uid=65532,gid=65532,mode=0700 "${D}-${v}" >/dev/null
    done
    start=$(date +%s.%N)
    docker run -d --name "${D}-runner" --network "${D}" --read-only --cap-drop ALL --security-opt no-new-privileges \
        --user 65532:65532 --memory 1536m --cpus 1 --pids-limit 256 \
        --tmpfs /tmp:rw,noexec,nosuid,size=64m --tmpfs /private:rw,noexec,nosuid,size=1g,uid=65532,gid=65532 \
        -v "${D}-workspace:/workspace" -v "${D}-control:/control" \
        -e FORGE_URL="http://${FORGE_USER}:${token}@forge:${FORGE_PORT}/${FORGE_USER}" \
        --entrypoint /opt/venv/bin/python "${RUNNER}" -c "import time; time.sleep(86400)" >/dev/null
    docker run -d --name "${D}-toolchain" --network "container:${D}-runner" --read-only --cap-drop ALL \
        --security-opt no-new-privileges --user 65532:65532 --memory 3g --cpus 2 --pids-limit 1024 \
        --mount "type=image,source=${AGENT},target=/rotor/bin" \
        --tmpfs /cache:rw,exec,nosuid,size=4g,uid=65532,gid=65532 \
        -v "${D}-workspace:/workspace" -v "${D}-control:/control" \
        -e HOME=/cache -e TMPDIR=/cache -e GOCACHE=/cache/go-build -e GOMODCACHE=/cache/mod -e GOTOOLCHAIN=local \
        -e GOFLAGS=-mod=readonly -e GOPROXY=https://proxy.golang.org \
        --entrypoint /rotor/bin/agent "${GOLANG}" serve /control/agent.sock >/dev/null
    until docker exec "${D}-runner" /opt/venv/bin/python -c 'import os; os.stat("/control/agent.sock")' 2>/dev/null; do
        sleep 0.1
    done
    echo "both containers up and the agent answering after $(awk "BEGIN{print $(date +%s.%N) - ${start}}") s"
}

dstep() { docker exec "${D}-runner" /opt/venv/bin/python /opt/trial/driver.py "$@"; }

docker_down() {
    docker rm -f "${D}-runner" "${D}-toolchain" "${D}-forge"
    docker volume rm "${D}-workspace" "${D}-control"
    docker network rm "${D}"
}

# Memory and CPU of the pod's containers, as the kubelet sees them.
usage() { kubectl get --raw "/api/v1/nodes/$(kubectl get pod -n "${NS}" "${POD}" -o jsonpath='{.spec.nodeName}')/proxy/stats/summary" |
    jq -c --arg p "${POD}" '.pods[] | select(.podRef.name == $p) | .containers[] | {name, memMB: (.memory.workingSetBytes / 1e6 | floor), rssMB: (.memory.rssBytes / 1e6 | floor)}'; }

"$@"
