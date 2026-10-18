# The GitHub workflows, checked by actionlint and the shellcheck it carries.
ACTIONLINT_IMAGE ?= rhysd/actionlint:1.7.7

.PHONY: check-actions
check-actions: ## Lint .github/workflows with actionlint
	docker run --rm -u $(UID_GID) -v $(ROOT):/repo:ro -w /repo $(ACTIONLINT_IMAGE) -no-color
