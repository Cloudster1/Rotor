# The compose stack. The project name and every published port derive from the checkout's path,
# so worktrees running their gates side by side never share a database or collide on a port.
STACK_HASH := $(shell printf '%s' '$(ROOT)' | cksum | cut -d' ' -f1)
STACK_PROJECT := rotor-$(STACK_HASH)
STACK_NET := $(STACK_PROJECT)_default

# Twenty ports per checkout from 30000 to 32759, above Exciter (10000s) and Stator (20000s).
STACK_PORT_BASE ?= $(shell $(ROOT)/scripts/stack-ports.sh '$(ROOT)')
stack_port = $(shell echo $$(($(STACK_PORT_BASE) + $(1))))

WEB_PORT      := $(call stack_port,0)
API_PORT      := $(call stack_port,1)
KEYCLOAK_PORT := $(call stack_port,2)
POSTGRES_PORT := $(call stack_port,3)
VALKEY_PORT   := $(call stack_port,4)
OPENBAO_PORT  := $(call stack_port,5)

# Credentials for the throwaway development stack; override them in the environment for anything else.
POSTGRES_PASSWORD  ?= rotor
APP_DB_PASSWORD    ?= rotor_app
ADMIN_DB_PASSWORD  ?= rotor_admin
VALKEY_PASSWORD    ?= rotor_valkey
OPENBAO_ROOT_TOKEN ?= rotor-dev-root

KEYCLOAK_REALM := dynamo
# Dynamo holds the suite's canonical realm; Armature, Stator and Exciter compare theirs with the same copy.
DYNAMO_REALM_URL ?= https://raw.githubusercontent.com/praetorianer777/Dynamo/main/deploy/keycloak/realm.json

.PHONY: realm-check
realm-check: ## Fail if deploy/keycloak/realm.json differs from Dynamo's canonical realm
	@docker run --rm -u $(UID_GID) -v $(ROOT)/deploy/keycloak:/realm:ro $(NODE_IMAGE) \
		node /realm/realm-check.mjs '$(DYNAMO_REALM_URL)' /realm/realm.json

export COMPOSE_FILE := $(ROOT)/deploy/docker-compose.yml
export COMPOSE_PROJECT_NAME := $(STACK_PROJECT)
export WEB_PORT API_PORT KEYCLOAK_PORT POSTGRES_PORT VALKEY_PORT OPENBAO_PORT
export POSTGRES_PASSWORD APP_DB_PASSWORD ADMIN_DB_PASSWORD VALKEY_PASSWORD OPENBAO_ROOT_TOKEN

STACK_PG_URL = postgres://$(1):$(2)@$(3):5432/rotor?sslmode=disable

# The Go toolchain container on the stack's network, with everything the integration suite reads.
DOCKER_GO_STACK = $(call go_run,--network $(STACK_NET) \
	-e ROTOR_DB_URL='$(call STACK_PG_URL,rotor_app,$(APP_DB_PASSWORD),postgres)' \
	-e ROTOR_DB_ADMIN_URL='$(call STACK_PG_URL,rotor_admin,$(ADMIN_DB_PASSWORD),postgres)' \
	-e ROTOR_TEST_SUPERUSER_URL='$(call STACK_PG_URL,rotor,$(POSTGRES_PASSWORD),postgres)' \
	-e ROTOR_TEST_API_URL=http://api:8080 \
	-e ROTOR_TEST_APP_URL=http://localhost:$(WEB_PORT) \
	-e ROTOR_TEST_KEYCLOAK_URL=http://keycloak:8080 \
	-e ROTOR_TEST_KEYCLOAK_PUBLIC_URL=http://localhost:$(KEYCLOAK_PORT) \
	-e ROTOR_TEST_OPENBAO_URL=http://openbao:8200 \
	-e ROTOR_TEST_OPENBAO_SEALED_URL=http://openbao-sealed:8200 \
	-e ROTOR_TEST_OPENBAO_ROOT_TOKEN='$(OPENBAO_ROOT_TOKEN)' \
	-e ROTOR_TEST_BUILD_VERSION='$(ROTOR_BUILD_VERSION)' \
	-e ROTOR_TEST_BUILD_COMMIT='$(ROTOR_BUILD_COMMIT)' \
	$(1))

# What the stack publishes, for the test suites and for people: a shell can source it, and so can Playwright.
STACK_ENV_FILE := $(ROOT)/.cache/stack.env

.PHONY: stack-env
stack-env:
	@mkdir -p $(dir $(STACK_ENV_FILE))
	@printf '%s\n' \
		'COMPOSE_PROJECT_NAME=$(STACK_PROJECT)' \
		'STACK_NETWORK=$(STACK_NET)' \
		'WEB_PORT=$(WEB_PORT)' \
		'API_PORT=$(API_PORT)' \
		'KEYCLOAK_PORT=$(KEYCLOAK_PORT)' \
		'POSTGRES_PORT=$(POSTGRES_PORT)' \
		'VALKEY_PORT=$(VALKEY_PORT)' \
		'OPENBAO_PORT=$(OPENBAO_PORT)' \
		'ROTOR_WEB_URL=http://localhost:$(WEB_PORT)' \
		'ROTOR_API_URL=http://localhost:$(API_PORT)' \
		'ROTOR_KEYCLOAK_URL=http://localhost:$(KEYCLOAK_PORT)' \
		'ROTOR_OIDC_ISSUER=http://localhost:$(KEYCLOAK_PORT)/realms/$(KEYCLOAK_REALM)' \
		'ROTOR_OPENBAO_URL=http://localhost:$(OPENBAO_PORT)' \
		'ROTOR_BUILD_VERSION=$(ROTOR_BUILD_VERSION)' \
		'ROTOR_BUILD_COMMIT=$(ROTOR_BUILD_COMMIT)' \
		'ROTOR_TEST_SUPERUSER_URL=postgres://rotor:$(POSTGRES_PASSWORD)@127.0.0.1:$(POSTGRES_PORT)/rotor?sslmode=disable' \
		> $(STACK_ENV_FILE)

# Prints a line only for services the compose file has, since api and web join the stack later.
STACK_SAY = docker compose config --services | grep -qx $(1) && echo $(2) || true

.PHONY: up
up: stack-env ## Build and start the whole stack in the background, and say where it is
	docker compose up -d --build --wait
	@$(call STACK_SAY,web,"Web        http://localhost:$(WEB_PORT)")
	@$(call STACK_SAY,api,"API        http://localhost:$(API_PORT)/api/v1/meta")
	@echo "Keycloak   http://localhost:$(KEYCLOAK_PORT) (admin / admin; realm $(KEYCLOAK_REALM); people alice, bob, carol, dave with password '<name> password')"
	@echo "OpenBao    http://localhost:$(OPENBAO_PORT) (root token $(OPENBAO_ROOT_TOKEN))"
	@echo "Postgres   127.0.0.1:$(POSTGRES_PORT)"
	@echo "Valkey     127.0.0.1:$(VALKEY_PORT)"
	@echo "Ports and project name are in $(STACK_ENV_FILE)."

.PHONY: down
down: ## Stop the stack, keeping its data
	docker compose down

.PHONY: clean
clean: ## Stop the stack and delete its volumes
	docker compose down -v --remove-orphans
	@rm -f $(STACK_ENV_FILE)

.PHONY: logs
logs: ## Follow the stack's logs: make logs S=keycloak for one service
	docker compose logs -f $(S)

.PHONY: ports
ports: ## Print this checkout's compose project and ports
	@echo "project $(STACK_PROJECT), ports $(STACK_PORT_BASE) to $(shell echo $$(($(STACK_PORT_BASE) + 19)))"

.PHONY: stack-up
stack-up: stack-env ## The gate's stack: build and start it, and wait until every service is healthy
	docker compose up -d --build --wait --quiet-pull

.PHONY: stack-down
stack-down: ## Remove the gate's stack and its volumes
	docker compose down -v --remove-orphans
	@rm -f $(STACK_ENV_FILE)
