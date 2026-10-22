# The Kubernetes sandbox backend's tests (#26), in a throwaway kind cluster: kind makes one out of a container on
# the host's Docker, so a machine needs only Docker. Heavy, so not part of ./run-tests.sh; .github/workflows/kube.yml
# runs them nightly, on pull requests that touch what a cluster runs, and on those labelled kube.
KIND_TOOLS_IMAGE ?= rotor-kind-tools:local
KIND_NODE_IMAGE ?= kindest/node:v1.34.0
KIND_CLUSTER ?= rotor-$(STACK_HASH)
# The cluster's network plugin, so the sandboxes' egress policies, hostname allows included, hold as in production.
CILIUM_REPO ?= https://helm.cilium.io
# renovate: datasource=helm depName=cilium registryUrl=https://helm.cilium.io
CILIUM_VERSION ?= 1.19.8
# The registry the chart install test pushes and signs Rotor's images in, and the admission controller that checks
# their signatures in the sandbox namespace (#42); the kubectl tag matches scripts/kind/Dockerfile's.
# renovate: datasource=docker depName=registry
KIND_REGISTRY_IMAGE ?= registry:3.1.2@sha256:ddf754342cfc8acc51a56d5d0ab6af06826461864460636d8bd5c546dab2a7b8
POLICY_CONTROLLER_REPO ?= https://sigstore.github.io/helm-charts
# renovate: datasource=helm depName=policy-controller registryUrl=https://sigstore.github.io/helm-charts
POLICY_CONTROLLER_CHART ?= 0.10.8
KIND_KUBECTL_VERSION ?= 1.34.1
KIND_KUBECONFIG := $(ROOT)/.cache/kind/kubeconfig

# The tools container, on the host's network so the cluster's API server and the node are reachable.
kind_run = docker run --rm --network host -u $(UID_GID) --group-add $(DOCKER_GID) \
	-v /var/run/docker.sock:/var/run/docker.sock -v $(ROOT):/work -e HOME=/tmp \
	-e KUBECONFIG=/work/.cache/kind/kubeconfig -w /work $(1) $(KIND_TOOLS_IMAGE)
DOCKER_KIND = $(call kind_run,)

.PHONY: kind-tools
kind-tools: ## Build the image with kind, kubectl and helm the Kubernetes tests run in
	docker build -q -t $(KIND_TOOLS_IMAGE) $(ROOT)/scripts/kind >/dev/null

.PHONY: kind-up
kind-up: kind-tools $(if $(wildcard $(ROOT)/runner/pyproject.toml),runner-image) ## Start this checkout's kind cluster, load the runner and agent images, add the sandbox namespace and RBAC, and gVisor
	@mkdir -p $(dir $(KIND_KUBECONFIG))
	$(DOCKER_KIND) sh -c 'kind get clusters 2>/dev/null | grep -qx $(KIND_CLUSTER) \
		|| kind create cluster --name $(KIND_CLUSTER) --image $(KIND_NODE_IMAGE) --config scripts/kind/cluster.yaml; \
		kind export kubeconfig --name $(KIND_CLUSTER) \
		&& helm repo add cilium $(CILIUM_REPO) --force-update >/dev/null \
		&& helm upgrade --install cilium cilium/cilium --version $(CILIUM_VERSION) -n kube-system \
			--set ipam.mode=kubernetes --set image.pullPolicy=IfNotPresent --set operator.replicas=1 --wait --timeout 10m \
		&& kubectl wait --for=condition=Ready nodes --all --timeout=300s \
		&& kind load docker-image $(RUNNER_IMAGE) $(AGENT_IMAGE) --name $(KIND_CLUSTER) \
		&& kubectl apply -f deploy/kube/sandboxes.yaml \
		&& CLUSTER=$(KIND_CLUSTER) scripts/kind/gvisor.sh'

.PHONY: kind-down
kind-down: kind-tools ## Delete this checkout's kind cluster and the registry beside it
	$(DOCKER_KIND) kind delete cluster --name $(KIND_CLUSTER)
	docker rm -f $(KIND_CLUSTER)-registry >/dev/null 2>&1 || true

.PHONY: kind-status
kind-status: kind-tools ## Show what runs in this checkout's kind cluster and what it reported lately
	$(DOCKER_KIND) sh -c 'kubectl get pods -A -o wide; kubectl get events -A --sort-by=.lastTimestamp | tail -40'

.PHONY: test-chart-install
test-chart-install: kind-tools ## Install the chart from signed images into this checkout's kind cluster, carry runs in sandbox pods and upgrade; KEEP=1 keeps it
	@[ -f $(KIND_KUBECONFIG) ] || { echo "No kind cluster for this checkout. Start it with make kind-up, then run this again."; exit 1; }
	$(call kind_run,-e CLUSTER=$(KIND_CLUSTER) -e RUNNER_IMAGE=$(RUNNER_IMAGE) -e KEEP -e REGISTRY_IMAGE=$(KIND_REGISTRY_IMAGE) \
		-e POLICY_CONTROLLER_REPO=$(POLICY_CONTROLLER_REPO) -e POLICY_CONTROLLER_CHART=$(POLICY_CONTROLLER_CHART) \
		-e KUBECTL_VERSION=$(KIND_KUBECTL_VERSION)) scripts/chart-install-test.sh

.PHONY: test-kube
test-kube: | $(GO_CACHE) ## Run the Kubernetes sandbox tests against this checkout's kind cluster; start it with make kind-up
	@[ -f $(KIND_KUBECONFIG) ] || { echo "No kind cluster for this checkout. Start it with make kind-up, then run this again."; exit 1; }
	@mkdir -p $(REPORTS)
	$(ROOT)/scripts/go-test.sh $(REPORTS)/kube.json $(call go_run,--network host \
		-e ROTOR_TEST_KUBECONFIG=/work/.cache/kind/kubeconfig -e ROTOR_TEST_KUBE_IMAGE=$(RUNNER_IMAGE) \
		-e ROTOR_TEST_AGENT_IMAGE=$(AGENT_IMAGE)) \
		go test -json -tags kube -count=1 $(TESTFLAGS) ./test/kube/...
