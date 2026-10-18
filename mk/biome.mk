# TypeScript formatting for web/ and e2e/ by the root biome.json, from Biome's own pinned image.
BIOME_IMAGE ?= ghcr.io/biomejs/biome:2.5.8
# Only the directories that exist: biome refuses a path that is not there.
BIOME_DIRS := $(notdir $(wildcard $(ROOT)/web $(ROOT)/e2e))

DOCKER_BIOME = docker run --rm -u $(UID_GID) -v $(ROOT):/code -w /code $(BIOME_IMAGE)

ifneq ($(BIOME_DIRS),)
.PHONY: check-format
check-format: ## Fail when a file in web/ or e2e/ is not formatted as biome.json says
	@$(DOCKER_BIOME) format $(BIOME_DIRS) \
		|| { echo "Some files in $(BIOME_DIRS) are not formatted. Run make format, review the changes and commit them."; exit 1; }

.PHONY: format
format: ## Format web/ and e2e/ in place as biome.json says
	$(DOCKER_BIOME) format --write $(BIOME_DIRS)
endif
