# What a release publishes besides the images (#44): the chart, packed and pushed to an OCI registry. Helm runs in a
# container like every other toolchain, the same image tests/test-helm.sh renders the chart with.
HELM_IMAGE ?= alpine/helm:3.19.0
CHART_DIR = $(ROOT)/deploy/charts/rotor
CHART_DIST ?= $(ROOT)/dist
CHART_VERSION = $(shell sed -n 's/^version: //p' $(CHART_DIR)/Chart.yaml)
# --plain-http for a local registry, as the release test pushes to; ghcr needs nothing.
CHART_PUSH_FLAGS ?=

.PHONY: chart-package
chart-package: ## Package deploy/charts/rotor into dist/ as rotor-<version>.tgz
	mkdir -p $(CHART_DIST)
	docker run --rm -u $(UID_GID) -e HOME=/tmp -v $(CHART_DIR):/chart:ro -v $(CHART_DIST):/dist $(HELM_IMAGE) \
		package /chart --destination /dist

# The digest lands next to the package, rotor-<version>.digest, so the release can sign the chart by digest.
.PHONY: chart-push
chart-push: ## Push dist/rotor-<version>.tgz to CHART_REPOSITORY (release.yml does this)
	@test -n "$$CHART_REPOSITORY" -a -n "$$REGISTRY_USER" -a -n "$$REGISTRY_TOKEN" \
		|| { echo "Set CHART_REPOSITORY (oci://host/path), REGISTRY_USER and REGISTRY_TOKEN; release.yml sets all three." >&2; exit 1; }
	docker run --rm --network host -u $(UID_GID) -e HOME=/tmp -e CHART_REPOSITORY -e REGISTRY_USER -e REGISTRY_TOKEN \
		-v $(CHART_DIST):/dist --entrypoint sh $(HELM_IMAGE) -c 'set -eo pipefail; \
		host=$${CHART_REPOSITORY#oci://}; host=$${host%%/*}; \
		printf %s "$$REGISTRY_TOKEN" | helm registry login "$$host" -u "$$REGISTRY_USER" --password-stdin $(CHART_PUSH_FLAGS); \
		helm push /dist/rotor-$(CHART_VERSION).tgz "$$CHART_REPOSITORY" $(CHART_PUSH_FLAGS) 2>&1 | tee /dist/push.log; \
		sed -n "s/^Digest: //p" /dist/push.log > /dist/rotor-$(CHART_VERSION).digest; rm -f /dist/push.log; \
		grep -q "^sha256:" /dist/rotor-$(CHART_VERSION).digest \
			|| { echo "helm push named no digest, so the chart cannot be signed by it." >&2; exit 1; }'
