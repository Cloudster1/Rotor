# The tools Rotor connects to, as the test double stands in for them: their API documents, vendored for the
# tool-stub's contract test, and a snapshot of Exciter's MCP tools/list from the suite. Adapted from Exciter's
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
tool-snapshots: ## Record a tool's tools/list for the stub: make tool-snapshots TOOL=exciter (the running suite's) or TOOL=armature
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

.PHONY: tool-snapshot-exciter
tool-snapshot-exciter:
	@docker network inspect $(SUITE_NET) >/dev/null 2>&1 \
		|| { echo "The suite is not up. Start the stack with make up, then make suite-up, then run this again."; exit 1; }
	@$(call snapshot_record,http://localhost:$(SUITE_EXCITER_WEB_PORT),,exciter)
	@echo "Recorded Exciter's tools/list at $(EXCITER_REF); run make check-go."

# Armature's list is built from its code alone, so a throwaway installation at ARMATURE_REF records it: its own
# compose with deploy/suite/armature-snapshot.yml over it, its first sign-up the owner, and gone again afterwards.
ARMATURE_REPO ?= https://github.com/Cloudster1/Armature.git
ARMATURE_REF := $(shell cat $(ROOT)/deploy/suite/ARMATURE_REF 2>/dev/null)
SNAPSHOT_ARMATURE_DIR := $(ROOT)/.cache/tools/armature
SNAPSHOT_ARMATURE = docker compose -p $(STACK_PROJECT)-armature-snapshot \
	-f $(SNAPSHOT_ARMATURE_DIR)/deploy/docker-compose.yml -f $(ROOT)/deploy/suite/armature-snapshot.yml
# A person of the throwaway installation alone, which is gone with it.
SNAPSHOT_ARMATURE_LOGIN := -e TOOL_SIGNUP_NAME=Snapshot -e TOOL_ADMIN_EMAIL=snapshot@armature.test \
	-e 'TOOL_ADMIN_PASSWORD=armature snapshot password'

.PHONY: tool-snapshot-armature
tool-snapshot-armature:
	@[ -n "$(ARMATURE_REF)" ] || { echo "deploy/suite/ARMATURE_REF names no commit."; exit 1; }
	@[ -d $(SNAPSHOT_ARMATURE_DIR)/.git ] || git clone --quiet --filter=blob:none --no-checkout $(ARMATURE_REPO) $(SNAPSHOT_ARMATURE_DIR)
	@[ -f $(SNAPSHOT_ARMATURE_DIR)/deploy/docker-compose.yml ] \
		&& [ "$$(git -C $(SNAPSHOT_ARMATURE_DIR) rev-parse -q --verify HEAD)" = "$(ARMATURE_REF)" ] || { \
		git -C $(SNAPSHOT_ARMATURE_DIR) fetch --quiet origin $(ARMATURE_REF) && \
		git -C $(SNAPSHOT_ARMATURE_DIR) -c advice.detachedHead=false checkout --quiet --detach FETCH_HEAD; }
	@trap '$(SNAPSHOT_ARMATURE) down -v --remove-orphans >/dev/null 2>&1' EXIT; \
	$(SNAPSHOT_ARMATURE) up -d --build --wait --quiet-pull api || exit 1; \
	port=$$($(SNAPSHOT_ARMATURE) port api 8080 | sed 's/.*://'); \
	$(call snapshot_record,http://localhost:$$port,$(SNAPSHOT_ARMATURE_LOGIN),armature)
	@echo "Recorded Armature's tools/list at $(ARMATURE_REF); vendor its document at the same commit with" \
		"make tool-contracts TOOL=armature REF=$(ARMATURE_REF), then run make check-go."
