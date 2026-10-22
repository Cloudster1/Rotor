# The real Exciter and Armature beside this checkout's stack (#8, #130), for the tests that a test double cannot
# settle. Each runs its own compose from a clone at the commit in deploy/suite/<TOOL>_REF, with deploy/suite/<tool>.yml
# over it; Stator joins once it is an OAuth authorization server. Adapted from Exciter's mk/suite.mk.
# EXCITER_REF=main or ARMATURE_REF=main on the command line runs against what is not tagged yet, as the nightly does.
EXCITER_REPO ?= https://github.com/Cloudster1/Exciter.git
EXCITER_REF := $(shell cat $(ROOT)/deploy/suite/EXCITER_REF 2>/dev/null)
ARMATURE_REPO ?= https://github.com/Cloudster1/Armature.git
ARMATURE_REF := $(shell cat $(ROOT)/deploy/suite/ARMATURE_REF 2>/dev/null)
SUITE_DIR := $(ROOT)/.cache/suite
SUITE_EXCITER_DIR := $(SUITE_DIR)/exciter
SUITE_ARMATURE_DIR := $(SUITE_DIR)/armature
SUITE_NET := $(STACK_PROJECT)-suite
SUITE_EXCITER_PROJECT := $(STACK_PROJECT)-exciter
SUITE_ARMATURE_PROJECT := $(STACK_PROJECT)-armature

# The siblings' ports come from this checkout's block, after Rotor's own, so suites of two worktrees never collide:
# Exciter takes 7 to 15, Armature publishes its web alone at 18 (16 and 17 are the observe profile's).
SUITE_EXCITER_WEB_PORT := $(call stack_port,7)
SUITE_ARMATURE_WEB_PORT := $(call stack_port,18)
SUITE_EXCITER = COMPOSE_PROJECT_NAME=$(SUITE_EXCITER_PROJECT) \
	COMPOSE_FILE=$(SUITE_EXCITER_DIR)/deploy/docker-compose.yml:$(ROOT)/deploy/suite/exciter.yml \
	WEB_PORT=$(SUITE_EXCITER_WEB_PORT) API_PORT=$(call stack_port,8) \
	KEYCLOAK_PORT=$(KEYCLOAK_PORT) MAILPIT_PORT=$(call stack_port,9) POSTGRES_PORT=$(call stack_port,10) \
	POSTGRES_REPLICA_PORT=$(call stack_port,11) VALKEY_PORT=$(call stack_port,12) S3_PORT=$(call stack_port,13) \
	ARMATURE_STUB_PORT=$(call stack_port,14) STATOR_STUB_PORT=$(call stack_port,15) docker compose
SUITE_ARMATURE = COMPOSE_PROJECT_NAME=$(SUITE_ARMATURE_PROJECT) \
	COMPOSE_FILE=$(SUITE_ARMATURE_DIR)/deploy/docker-compose.yml:$(ROOT)/deploy/suite/armature.yml \
	WEB_PORT=$(SUITE_ARMATURE_WEB_PORT) KEYCLOAK_PORT=$(KEYCLOAK_PORT) docker compose

# Rotor's services the suite reaches, each joining the suite's network as rotor-<service>, and the siblings', as
# suite-<tool>-<service>; by hand, so no service is known there by a name another stack uses too. Rotor's worker is
# among them because it revokes a run's tokens at the tools when the run ends; Exciter's and Armature's workers
# deliver webhooks to rotor-api (#35), and Exciter's api reaches Armature's to trace requirements to issues.
SUITE_ROTOR_SERVICES := api worker keycloak postgres openbao tool-stub
SUITE_EXCITER_SERVICES := api web worker
SUITE_ARMATURE_SERVICES := api worker

# suite_join connects services $(3) of compose $(1) to network $(4) as $(2)<service>, once each.
define suite_join
	@for s in $(3); do \
		id=$$($(1) ps -q $$s 2>/dev/null); [ -n "$$id" ] || continue; \
		docker inspect -f '{{range $$n, $$_ := .NetworkSettings.Networks}}{{$$n}} {{end}}' $$id | grep -qw $(4) \
			|| docker network connect --alias $(2)$$s $(4) $$id; \
	done
endef

# suite_leave disconnects services $(2) of compose $(1) from network $(3), when they are there.
define suite_leave
	@for s in $(2); do \
		id=$$($(1) ps -q $$s 2>/dev/null); [ -z "$$id" ] || docker network disconnect $(3) $$id 2>/dev/null || true; \
	done
endef

# suite_clone checks out $(2) of repository $(1) in $(3), fetching only when the checkout is elsewhere.
define suite_clone
	@[ -d $(3)/.git ] || git clone --quiet --filter=blob:none --no-checkout $(1) $(3)
	@[ -f $(3)/deploy/docker-compose.yml ] && [ "$$(git -C $(3) rev-parse -q --verify HEAD)" = "$(2)" ] || { \
		git -C $(3) fetch --quiet origin $(2) && \
		git -C $(3) -c advice.detachedHead=false checkout --quiet --detach FETCH_HEAD; }
endef

# A run's sandbox calls a tool at the address the api rewrites it to, so Exciter's web and Armature's api join the
# sandboxes' internal network under that name too (#25); nothing else of the suite does.
SUITE_SANDBOX_NET := $(STACK_PROJECT)_sandbox

# The seed reaches Armature and Exciter on the suite's network; the issuer is the one Rotor's Keycloak names.
SUITE_SEED = $(call go_run,--network $(SUITE_NET) \
	-e SUITE_KEYCLOAK_ISSUER=http://localhost:$(KEYCLOAK_PORT)/realms/$(KEYCLOAK_REALM) \
	-e SUITE_ARMATURE_URL=http://localhost:$(SUITE_ARMATURE_WEB_PORT)) go run ./cmd/suite-seed

export SUITE_EXCITER_WEB_PORT SUITE_ARMATURE_WEB_PORT

.PHONY: suite-up
suite-up: | $(GO_CACHE) ## Start the real Exciter and Armature beside this checkout's running stack, signing in through its Keycloak
	@docker compose ps --status running --services 2>/dev/null | grep -qx keycloak \
		|| { echo "The stack for this checkout is not running. Start it with make up or make stack-up, then run this again."; exit 1; }
	@[ -n "$(EXCITER_REF)" ] || { echo "deploy/suite/EXCITER_REF names no commit."; exit 1; }
	@[ -n "$(ARMATURE_REF)" ] || { echo "deploy/suite/ARMATURE_REF names no commit."; exit 1; }
	$(call suite_clone,$(EXCITER_REPO),$(EXCITER_REF),$(SUITE_EXCITER_DIR))
	$(call suite_clone,$(ARMATURE_REPO),$(ARMATURE_REF),$(SUITE_ARMATURE_DIR))
	@docker network inspect $(SUITE_NET) >/dev/null 2>&1 || docker network create $(SUITE_NET) >/dev/null
	$(call suite_join,docker compose,rotor-,$(SUITE_ROTOR_SERVICES),$(SUITE_NET))
	$(SUITE_EXCITER) up -d --build --wait --quiet-pull
	$(call suite_join,$(SUITE_EXCITER),suite-exciter-,$(SUITE_EXCITER_SERVICES),$(SUITE_NET))
	$(call suite_join,$(SUITE_EXCITER),suite-exciter-,web,$(SUITE_SANDBOX_NET))
	$(SUITE_ARMATURE) up -d --build --wait --quiet-pull
	$(call suite_join,$(SUITE_ARMATURE),suite-armature-,$(SUITE_ARMATURE_SERVICES),$(SUITE_NET))
	$(call suite_join,$(SUITE_ARMATURE),suite-armature-,api,$(SUITE_SANDBOX_NET))
	$(SUITE_SEED)
	@echo "Exciter    http://localhost:$(SUITE_EXCITER_WEB_PORT) ($(EXCITER_REF); sign in as alice or bob through Rotor's Keycloak)"
	@echo "Armature   http://localhost:$(SUITE_ARMATURE_WEB_PORT) ($(ARMATURE_REF); project CP; sign in as alice, bob or dave the same way)"

.PHONY: suite-down
suite-down: ## Stop the suite's Exciter and Armature, keeping their data
	$(call suite_leave,docker compose,$(SUITE_ROTOR_SERVICES),$(SUITE_NET))
	$(call suite_leave,$(SUITE_EXCITER),web,$(SUITE_SANDBOX_NET))
	$(call suite_leave,$(SUITE_ARMATURE),api,$(SUITE_SANDBOX_NET))
	@[ ! -f $(SUITE_ARMATURE_DIR)/deploy/docker-compose.yml ] || $(SUITE_ARMATURE) down
	$(SUITE_EXCITER) down

.PHONY: suite-logs
suite-logs: ## Print the suite's logs: make suite-logs S=api for one service of each, T=exciter or T=armature for one tool
	@[ "$(T)" = armature ] || { echo "== Exciter"; $(SUITE_EXCITER) logs --tail 200 $(S); }
	@[ "$(T)" = exciter ] || [ ! -f $(SUITE_ARMATURE_DIR)/deploy/docker-compose.yml ] \
		|| { echo "== Armature"; $(SUITE_ARMATURE) logs --tail 200 $(S); }

.PHONY: suite-clean
suite-clean: suite-down ## Stop the suite's Exciter and Armature and forget their data
	@[ ! -f $(SUITE_ARMATURE_DIR)/deploy/docker-compose.yml ] || $(SUITE_ARMATURE) down -v --remove-orphans
	$(SUITE_EXCITER) down -v --remove-orphans
	@docker network rm $(SUITE_NET) >/dev/null 2>&1 || true

# The Go toolchain on the suite's network, which reaches Rotor under rotor-* and the siblings under suite-*, with the
# Docker socket to watch and stop the stack's sandboxes and worker, as the integration suite has.
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
	-e ROTOR_TEST_ARMATURE_URL=http://localhost:$(SUITE_ARMATURE_WEB_PORT) \
	-e ROTOR_TEST_ARMATURE_INTERNAL_URL=http://suite-armature-api:8080 \
	$(1))

.PHONY: test-suite
test-suite: | $(GO_CACHE) ## Run the tests against the real Exciter and Armature; start them first with make suite-up
	@docker network inspect $(SUITE_NET) >/dev/null 2>&1 \
		|| { echo "The suite is not up. Start the stack with make up, then make suite-up, then run this again."; exit 1; }
	$(ROOT)/tests/test-suite.sh $(SUITE_EXCITER_WEB_PORT) $(SUITE_ARMATURE_WEB_PORT)
	$(ROOT)/scripts/go-test.sh $(REPORTS)/suite.json $(DOCKER_GO_SUITE) go test -json -tags 'integration suite' -run Suite -count=1 $(TESTFLAGS) ./test/...
