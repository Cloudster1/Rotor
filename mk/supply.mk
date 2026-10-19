# The supply chain (#42): what an image is made of, that it is the one Rotor built, and the known vulnerabilities in
# what Rotor depends on. Signing runs in the kind tools image, which carries cosign and syft; nothing on the host.

# Images by digest, such as IMAGES="ghcr.io/cloudster1/rotor-runner@sha256:...", and where their SBOMs go.
IMAGES ?=
SBOM_DIR ?= reports/sbom
# The key pair the images are signed with; a password protected key reads COSIGN_PASSWORD from the environment.
COSIGN_KEY ?=
COSIGN_PUB ?=
# 1 when the registry speaks plain HTTP, as a local one does.
REGISTRY_HTTP ?=

# renovate: datasource=go depName=golang.org/x/vuln
GOVULNCHECK_VERSION ?= v1.8.0
# Moved by hand with the date below, which must not fall before its release.
PIP_AUDIT_VERSION ?= 2.10.1
# pip-audit's own dependencies, resolved as they stood then, so a release from this morning never runs here.
PIP_AUDIT_EXCLUDE_NEWER ?= 2026-10-05T00:00:00Z
# The npm advisories that fail the audit; lower ones are reported and left to Renovate.
NPM_AUDIT_LEVEL ?= high

supply_run = $(call kind_run,-e SBOM_DIR=$(SBOM_DIR) -e COSIGN_KEY=$(COSIGN_KEY) -e COSIGN_PUB=$(COSIGN_PUB) \
	-e COSIGN_PASSWORD -e REGISTRY_HTTP=$(REGISTRY_HTTP)) scripts/supply.sh $(1) $(IMAGES)

.PHONY: sbom
sbom: kind-tools ## Write the SPDX SBOM of each of IMAGES into reports/sbom
	$(call supply_run,sbom)

.PHONY: sign
sign: kind-tools ## Sign each of IMAGES with the cosign key COSIGN_KEY
	$(call supply_run,sign)

.PHONY: attest
attest: kind-tools ## Attach each of IMAGES' SBOM to it as an attestation signed with COSIGN_KEY
	$(call supply_run,attest)

.PHONY: verify
verify: kind-tools ## Check each of IMAGES' signature and SBOM attestation against COSIGN_PUB
	$(call supply_run,verify)

# Each audit asks its ecosystem's advisory database, so it needs the network; deps.yml runs them in CI.
.PHONY: audit-deps
audit-deps: audit-go audit-python audit-npm ## Look for known vulnerabilities in the Go, Python and npm dependencies

# govulncheck reports only the vulnerable code Rotor's binaries can reach, which keeps it quiet enough to fail on.
.PHONY: audit-go
audit-go: | $(GO_CACHE)
	$(DOCKER_GO) go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

# What ships, from the lock file with its hashes, so nothing is installed; the npm audit draws the same line.
.PHONY: audit-python
audit-python: | $(UV_CACHE)
	$(DOCKER_UV) sh -c 'uv export --frozen --no-dev --no-emit-project --format requirements-txt -o /tmp/requirements.txt -q \
		&& uvx --exclude-newer $(PIP_AUDIT_EXCLUDE_NEWER) --from pip-audit==$(PIP_AUDIT_VERSION) pip-audit --require-hashes --disable-pip -r /tmp/requirements.txt'

# The web client's runtime dependencies; e2e/ ships nothing, as everything it holds is a development dependency.
.PHONY: audit-npm
audit-npm:
	@mkdir -p $(NPM_CACHE)
	$(DOCKER_NODE) npm audit --omit=dev --audit-level=$(NPM_AUDIT_LEVEL)
