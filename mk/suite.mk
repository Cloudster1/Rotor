# The real Exciter beside this checkout's stack (#8), for the tests that a test double cannot settle. Exciter runs
# its own compose from a clone at the commit in deploy/suite/EXCITER_REF, with deploy/suite/exciter.yml over it;
# Armature and Stator join once they are OAuth authorization servers. Adapted from Exciter's mk/suite.mk.
EXCITER_REPO ?= https://github.com/Cloudster1/Exciter.git
EXCITER_REF := $(shell cat $(ROOT)/deploy/suite/EXCITER_REF 2>/dev/null)
SUITE_DIR := $(ROOT)/.cache/suite
SUITE_EXCITER_DIR := $(SUITE_DIR)/exciter
SUITE_NET := $(STACK_PROJECT)-suite
SUITE_EXCITER_PROJECT := $(STACK_PROJECT)-exciter

# Exciter's ports come from this checkout's block, after Rotor's own, so suites of two worktrees never collide.
SUITE_EXCITER_WEB_PORT := $(call stack_port,7)
SUITE_EXCITER = COMPOSE_PROJECT_NAME=$(SUITE_EXCITER_PROJECT) \
	COMPOSE_FILE=$(SUITE_EXCITER_DIR)/deploy/docker-compose.yml:$(ROOT)/deploy/suite/exciter.yml \
	WEB_PORT=$(SUITE_EXCITER_WEB_PORT) API_PORT=$(call stack_port,8) \
	KEYCLOAK_PORT=$(KEYCLOAK_PORT) MAILPIT_PORT=$(call stack_port,9) POSTGRES_PORT=$(call stack_port,10) \
	POSTGRES_REPLICA_PORT=$(call stack_port,11) VALKEY_PORT=$(call stack_port,12) S3_PORT=$(call stack_port,13) \
	ARMATURE_STUB_PORT=$(call stack_port,14) STATOR_STUB_PORT=$(call stack_port,15) docker compose

# Rotor's services the suite reaches, each joining the suite's network as rotor-<service>, and Exciter's, as
# suite-exciter-<service>; by hand, so no service is known there by a name another stack uses too. Rotor's worker is
# among them because it revokes a run's tokens at Exciter when the run ends, Exciter's because it delivers webhooks
# to rotor-api (#35).
SUITE_ROTOR_SERVICES := api worker keycloak postgres openbao tool-stub
SUITE_EXCITER_SERVICES := api web worker

# suite_join connects services $(3) of compose $(1) to the suite's network as $(2)<service>, once each.
define suite_join
	@for s in $(3); do \
		id=$$($(1) ps -q $$s 2>/dev/null); [ -n "$$id" ] || continue; \
		docker inspect -f '{{range $$n, $$_ := .NetworkSettings.Networks}}{{$$n}} {{end}}' $$id | grep -qw $(SUITE_NET) \
			|| docker network connect --alias $(2)$$s $(SUITE_NET) $$id; \
	done
endef

# A run's sandbox calls Exciter at the address the api rewrites it to, so Exciter's web joins the sandboxes'
# internal network under that name too (#25); nothing else of the suite does.
SUITE_SANDBOX_NET := $(STACK_PROJECT)_sandbox
define suite_sandbox_join
	@id=$$($(SUITE_EXCITER) ps -q web 2>/dev/null); [ -z "$$id" ] \
		|| docker inspect -f '{{range $$n, $$_ := .NetworkSettings.Networks}}{{$$n}} {{end}}' $$id | grep -qw $(SUITE_SANDBOX_NET) \
		|| docker network connect --alias suite-exciter-web $(SUITE_SANDBOX_NET) $$id
endef

export SUITE_EXCITER_WEB_PORT

.PHONY: suite-up
suite-up: ## Start the real Exciter beside this checkout's running stack, signing in through its Keycloak
	@docker compose ps --status running --services 2>/dev/null | grep -qx keycloak \
		|| { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }
	@[ -n "$(EXCITER_REF)" ] || { echo "deploy/suite/EXCITER_REF names no commit."; exit 1; }
	@[ -d $(SUITE_EXCITER_DIR)/.git ] || git clone --quiet --filter=blob:none --no-checkout $(EXCITER_REPO) $(SUITE_EXCITER_DIR)
	@[ -f $(SUITE_EXCITER_DIR)/deploy/docker-compose.yml ] \
		&& [ "$$(git -C $(SUITE_EXCITER_DIR) rev-parse -q --verify HEAD)" = "$(EXCITER_REF)" ] || { \
		git -C $(SUITE_EXCITER_DIR) fetch --quiet origin $(EXCITER_REF) && \
		git -C $(SUITE_EXCITER_DIR) -c advice.detachedHead=false checkout --quiet --detach $(EXCITER_REF); }
	@docker network inspect $(SUITE_NET) >/dev/null 2>&1 || docker network create $(SUITE_NET) >/dev/null
	$(call suite_join,docker compose,rotor-,$(SUITE_ROTOR_SERVICES))
	$(SUITE_EXCITER) up -d --build --wait --quiet-pull
	$(call suite_join,$(SUITE_EXCITER),suite-exciter-,$(SUITE_EXCITER_SERVICES))
	$(call suite_sandbox_join)
	@echo "Exciter    http://localhost:$(SUITE_EXCITER_WEB_PORT) ($(EXCITER_REF); sign in as alice or bob through Rotor's Keycloak)"

.PHONY: suite-down
suite-down: ## Stop the suite's Exciter, keeping its data
	@for s in $(SUITE_ROTOR_SERVICES); do \
		id=$$(docker compose ps -q $$s 2>/dev/null); [ -z "$$id" ] || docker network disconnect $(SUITE_NET) $$id 2>/dev/null || true; \
	done
	@id=$$($(SUITE_EXCITER) ps -q web 2>/dev/null); [ -z "$$id" ] || docker network disconnect $(SUITE_SANDBOX_NET) $$id 2>/dev/null || true
	$(SUITE_EXCITER) down

.PHONY: suite-logs
suite-logs: ## Print the suite's Exciter logs: make suite-logs S=api for one service
	$(SUITE_EXCITER) logs --tail 200 $(S)

.PHONY: suite-clean
suite-clean: suite-down ## Stop the suite's Exciter and forget its data
	$(SUITE_EXCITER) down -v --remove-orphans
	@docker network rm $(SUITE_NET) >/dev/null 2>&1 || true

# The Go toolchain on the suite's network, which reaches Rotor under rotor-* and Exciter under suite-exciter-*,
# with the Docker socket to watch and stop the stack's sandboxes and worker, as the integration suite has.
DOCKER_GO_SUITE = $(call go_run,--network $(SUITE_NET) \
	-v /var/run/docker.sock:/var/run/docker.sock --group-add $(DOCKER_GID) \
	-e ROTOR_TEST_SANDBOX_NETWORK=$(STACK_PROJECT)_sandbox \
	-e ROTOR_TEST_SANDBOX_STACK=$(STACK_PROJECT) \
	-e ROTOR_TEST_SANDBOX_IMAGE=rotor-runner:$(STACK_PROJECT) \
	-e ROTOR_TEST_OPENBAO_ROOT_TOKEN='$(OPENBAO_ROOT_TOKEN)' \
	-e ROTOR_DB_URL='$(call STACK_PG_URL,rotor_app,$(APP_DB_PASSWORD),rotor-postgres)' \
	-e ROTOR_DB_ADMIN_URL='$(call STACK_PG_URL,rotor_admin,$(ADMIN_DB_PASSWORD),rotor-postgres)' \
	-e ROTOR_TEST_SUPERUSER_URL='$(call STACK_PG_URL,rotor,$(POSTGRES_PASSWORD),rotor-postgres)' \
	-e ROTOR_TEST_API_URL=http://rotor-api:8080 \
	-e ROTOR_TEST_APP_URL=http://localhost:$(WEB_PORT) \
	-e ROTOR_TEST_KEYCLOAK_URL=http://rotor-keycloak:8080 \
	-e ROTOR_TEST_KEYCLOAK_PUBLIC_URL=http://localhost:$(KEYCLOAK_PORT) \
	-e ROTOR_TEST_OPENBAO_URL=http://rotor-openbao:8200 \
	-e ROTOR_TEST_TOOLSTUB_URL=http://localhost:$(TOOLSTUB_PORT) \
	-e ROTOR_TEST_TOOLSTUB_INTERNAL_URL=http://rotor-tool-stub:8080 \
	-e ROTOR_TEST_TOOLSTUB_CONTROL_URL=http://rotor-tool-stub:8081 \
	-e ROTOR_TEST_EXCITER_URL=http://localhost:$(SUITE_EXCITER_WEB_PORT) \
	-e ROTOR_TEST_EXCITER_INTERNAL_URL=http://suite-exciter-web:80 \
	$(1))

.PHONY: test-suite
test-suite: | $(GO_CACHE) ## Run the tests against the real Exciter; start it first with make suite-up
	@docker network inspect $(SUITE_NET) >/dev/null 2>&1 \
		|| { echo "The suite is not up. Start the stack with make up, then make suite-up, then run this again."; exit 1; }
	$(ROOT)/tests/test-suite.sh $(SUITE_EXCITER_WEB_PORT)
	$(ROOT)/scripts/go-test.sh $(REPORTS)/suite.json $(DOCKER_GO_SUITE) go test -json -tags 'integration suite' -run Suite -count=1 $(TESTFLAGS) ./test/...
