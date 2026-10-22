# The tools Rotor connects to, as the test double stands in for them: their API documents, vendored for the
# tool-stub's contract test, and a snapshot of each one's MCP tools/list beside it. Adapted from Exciter's
# mk/armature.mk: updating a vendored document is how a change in a tool reaches Rotor's test double.
CURL_IMAGE ?= curlimages/curl:8.16.0
TOOLS_API_DIR := $(ROOT)/api/tools
TOOL_REPO_exciter := Cloudster1/Exciter
TOOL_REPO_armature := Cloudster1/Armature
TOOL_REPO_stator := praetorianer777/Stator

.PHONY: tool-contracts
tool-contracts: ## Vendor a tool's api/openapi.json at a commit: make tool-contracts TOOL=exciter|armature|stator REF=<sha or branch>
	@[ -n "$(TOOL_REPO_$(TOOL))" ] || { echo "Name the tool: make tool-contracts TOOL=exciter|armature|stator REF=<sha or branch>"; exit 1; }
	@[ -n "$(REF)" ] || { echo "Name the commit to vendor: make tool-contracts TOOL=$(TOOL) REF=<sha or branch>"; exit 1; }
	@mkdir -p $(TOOLS_API_DIR)/$(TOOL)
	@sha=$$(docker run --rm $(CURL_IMAGE) -fsSL -H 'Accept: application/vnd.github.sha' \
		https://api.github.com/repos/$(TOOL_REPO_$(TOOL))/commits/$(REF)) \
		|| { echo "GitHub does not know $(REF) in $(TOOL_REPO_$(TOOL))."; exit 1; }; \
	docker run --rm $(CURL_IMAGE) -fsSL https://raw.githubusercontent.com/$(TOOL_REPO_$(TOOL))/$$sha/api/openapi.json \
		> $(TOOLS_API_DIR)/$(TOOL)/openapi.json.tmp \
		&& mv $(TOOLS_API_DIR)/$(TOOL)/openapi.json.tmp $(TOOLS_API_DIR)/$(TOOL)/openapi.json \
		&& printf '%s %s api/openapi.json\n' '$(TOOL_REPO_$(TOOL))' "$$sha" > $(TOOLS_API_DIR)/$(TOOL)/SOURCE \
		&& echo "Vendored $(TOOL_REPO_$(TOOL)) $$sha; run make check-go to see what the stub has to follow."

# Nightly against the siblings' main (#43): vendored into the working copy only, which CI never commits.
.PHONY: tool-contracts-main
tool-contracts-main: ## Vendor every tool's api/openapi.json at its main, to test against what is not tagged yet
	@for tool in exciter armature stator; do $(MAKE) --no-print-directory tool-contracts TOOL=$$tool REF=main || exit 1; done

.PHONY: test-contracts
test-contracts: | $(GO_CACHE) ## Hold the tool-stub and what Rotor reads of each tool to the vendored documents
	$(DOCKER_GO) go test -count=1 ./internal/toolstub/ ./internal/tools/

.PHONY: tool-snapshots
tool-snapshots: ## Record a tool's tools/list for the stub from a throwaway installation: make tool-snapshots TOOL=exciter|armature
	@case "$(or $(TOOL),exciter)" in \
		exciter) $(MAKE) --no-print-directory tool-snapshot-exciter;; \
		armature) $(MAKE) --no-print-directory tool-snapshot-armature;; \
		*) echo "Name the tool: make tool-snapshots TOOL=exciter|armature"; exit 1;; \
	esac

# snapshot_record runs the recorder at address $(1) with docker run options $(2) into api/tools/$(3)/tools-list.json.
define snapshot_record
	docker run --rm --network host -u $(UID_GID) -v $(ROOT)/scripts:/scripts:ro $(2) $(NODE_IMAGE) \
		node /scripts/tool-snapshot.mjs $(1) > $(TOOLS_API_DIR)/$(3)/tools-list.json.tmp \
		&& mv $(TOOLS_API_DIR)/$(3)/tools-list.json.tmp $(TOOLS_API_DIR)/$(3)/tools-list.json \
		|| { rm -f $(TOOLS_API_DIR)/$(3)/tools-list.json.tmp; exit 1; }
endef

# A tool's list is built from its code alone, so a throwaway installation at the suite's commit records it: its own
# compose with deploy/suite/<tool>-snapshot.yml over it, gone again afterwards. The repositories and commits are the
# suite's, mk/suite.mk.
SNAPSHOT_DIR := $(ROOT)/.cache/tools
snapshot_compose = docker compose -p $(STACK_PROJECT)-$(1)-snapshot \
	-f $(SNAPSHOT_DIR)/$(1)/deploy/docker-compose.yml -f $(ROOT)/deploy/suite/$(1)-snapshot.yml

# snapshot_throwaway records tool $(1) at commit $(3) of repository $(2), the recorder taking docker run options $(4).
define snapshot_throwaway
	@[ -n "$(3)" ] || { echo "deploy/suite names no commit of $(1) to record."; exit 1; }
	$(call suite_clone,$(2),$(3),$(SNAPSHOT_DIR)/$(1))
	@trap '$(call snapshot_compose,$(1)) down -v --remove-orphans >/dev/null 2>&1' EXIT; \
	$(call snapshot_compose,$(1)) up -d --build --wait --quiet-pull api || exit 1; \
	port=$$($(call snapshot_compose,$(1)) port api 8080 | sed 's/.*://'); \
	$(call snapshot_record,http://localhost:$$port,$(4),$(1))
	@echo "Recorded $(1)'s tools/list at $(3); vendor its document at the same commit with" \
		"make tool-contracts TOOL=$(1) REF=$(3), then run make check-go."
endef

# Exciter's seed makes its bootstrap administrator, whom the recorder signs in as unless told otherwise.
.PHONY: tool-snapshot-exciter
tool-snapshot-exciter:
	$(call snapshot_throwaway,exciter,$(EXCITER_REPO),$(EXCITER_REF),)

# Armature's first sign-up is its owner: a person of the throwaway installation alone, which is gone with it.
SNAPSHOT_ARMATURE_LOGIN := -e TOOL_SIGNUP_NAME=Snapshot -e TOOL_ADMIN_EMAIL=snapshot@armature.test \
	-e 'TOOL_ADMIN_PASSWORD=armature snapshot password'

.PHONY: tool-snapshot-armature
tool-snapshot-armature:
	$(call snapshot_throwaway,armature,$(ARMATURE_REPO),$(ARMATURE_REF),$(SNAPSHOT_ARMATURE_LOGIN))
