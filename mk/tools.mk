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

.PHONY: tool-snapshots
tool-snapshots: ## Record the running suite's Exciter tools/list into api/tools/exciter/tools-list.json
	@docker network inspect $(SUITE_NET) >/dev/null 2>&1 \
		|| { echo "The suite is not up. Start the stack with make up, then make suite-up, then run this again."; exit 1; }
	@docker run --rm --network host -u $(UID_GID) -v $(ROOT)/scripts:/scripts:ro $(NODE_IMAGE) \
		node /scripts/tool-snapshot.mjs http://localhost:$(SUITE_EXCITER_WEB_PORT) > $(TOOLS_API_DIR)/exciter/tools-list.json.tmp \
		&& mv $(TOOLS_API_DIR)/exciter/tools-list.json.tmp $(TOOLS_API_DIR)/exciter/tools-list.json \
		&& echo "Recorded Exciter's tools/list at $(EXCITER_REF); run make check-go." \
		|| { rm -f $(TOOLS_API_DIR)/exciter/tools-list.json.tmp; exit 1; }
