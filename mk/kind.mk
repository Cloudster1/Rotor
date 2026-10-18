# The Kubernetes sandbox backend's tests (#26), in a throwaway kind cluster: kind makes one out of a container on
# the host's Docker, so a machine needs only Docker. Heavy, so not part of ./run-tests.sh; .github/workflows/kube.yml
# runs them nightly, on pull requests that touch what a cluster runs, and on those labelled kube.
KIND_TOOLS_IMAGE ?= rotor-kind-tools:local
KIND_NODE_IMAGE ?= kindest/node:v1.34.0
KIND_CLUSTER ?= rotor-$(STACK_HASH)
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
kind-up: kind-tools $(if $(wildcard $(ROOT)/runner/pyproject.toml),runner-image) ## Start this checkout's kind cluster, load the runner image and add the sandbox namespace and RBAC
	@mkdir -p $(dir $(KIND_KUBECONFIG))
	$(DOCKER_KIND) sh -c 'kind get clusters 2>/dev/null | grep -qx $(KIND_CLUSTER) \
		|| kind create cluster --name $(KIND_CLUSTER) --image $(KIND_NODE_IMAGE) --config scripts/kind/cluster.yaml --wait 180s; \
		kind export kubeconfig --name $(KIND_CLUSTER) \
		&& kind load docker-image $(RUNNER_IMAGE) --name $(KIND_CLUSTER) \
		&& kubectl apply -f deploy/kube/sandboxes.yaml'

.PHONY: kind-down
kind-down: kind-tools ## Delete this checkout's kind cluster
	$(DOCKER_KIND) kind delete cluster --name $(KIND_CLUSTER)

.PHONY: kind-status
kind-status: kind-tools ## Show what runs in this checkout's kind cluster and what it reported lately
	$(DOCKER_KIND) sh -c 'kubectl get pods -A -o wide; kubectl get events -A --sort-by=.lastTimestamp | tail -40'

.PHONY: test-chart-install
test-chart-install: kind-tools ## Install the chart into this checkout's kind cluster, carry a run in a sandbox pod and upgrade; KEEP=1 keeps it
	@[ -f $(KIND_KUBECONFIG) ] || { echo "No kind cluster for this checkout. Start it with make kind-up, then run this again."; exit 1; }
	$(call kind_run,-e CLUSTER=$(KIND_CLUSTER) -e RUNNER_IMAGE=$(RUNNER_IMAGE) -e KEEP) scripts/chart-install-test.sh

.PHONY: test-kube
test-kube: | $(GO_CACHE) ## Run the Kubernetes sandbox tests against this checkout's kind cluster; start it with make kind-up
	@[ -f $(KIND_KUBECONFIG) ] || { echo "No kind cluster for this checkout. Start it with make kind-up, then run this again."; exit 1; }
	@mkdir -p $(REPORTS)
	$(ROOT)/scripts/go-test.sh $(REPORTS)/kube.json $(call go_run,--network host \
		-e ROTOR_TEST_KUBECONFIG=/work/.cache/kind/kubeconfig -e ROTOR_TEST_KUBE_IMAGE=$(RUNNER_IMAGE)) \
		go test -json -tags kube -count=1 $(TESTFLAGS) ./test/kube/...
