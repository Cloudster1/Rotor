# The Python runner, run in uv's pinned image as the invoking user with the uv cache under .cache/.
# Defined only once runner/pyproject.toml exists, so run-tests.sh skips check-python before then.
ifneq ($(wildcard $(ROOT)/runner/pyproject.toml),)

UV_IMAGE ?= ghcr.io/astral-sh/uv:0.9.30-python3.13-bookworm-slim@sha256:531f855bda2c73cd6ef67d56b733b357cea384185b3022bd09f05e002cd144ca
UV_CACHE := $(ROOT)/.cache/uv
# Tagged by stack, so worktrees sharing one Docker daemon never run each other's runner.
RUNNER_IMAGE ?= rotor-runner:$(STACK_PROJECT)

# The image's own Python, so nothing is downloaded at run time; copies, since the cache sits on another mount.
uv_run = docker run --rm \
	-u $(UID_GID) \
	-v $(ROOT):/work \
	-v $(UV_CACHE):/cache \
	-e HOME=/tmp \
	-e UV_CACHE_DIR=/cache \
	-e UV_PYTHON_DOWNLOADS=never \
	-e UV_LINK_MODE=copy \
	$(1) \
	-w /work/runner $(UV_IMAGE)
DOCKER_UV = $(call uv_run,)

$(UV_CACHE):
	@mkdir -p $@

.PHONY: uv-lock
uv-lock: | $(UV_CACHE) ## Update runner/uv.lock after changing runner/pyproject.toml
	$(DOCKER_UV) uv lock

.PHONY: py-fmt
py-fmt: | $(UV_CACHE) ## Format the runner and apply ruff's safe fixes in place
	$(DOCKER_UV) sh -c 'uv sync --locked --quiet && uv run --no-sync ruff format . && uv run --no-sync ruff check --fix .'

.PHONY: runner-image
runner-image: agent-image ## Build the runner's sandbox image, and the exec agent its workspaces start
	docker build -q -f $(ROOT)/deploy/Dockerfile.runner -t $(RUNNER_IMAGE) $(ROOT) >/dev/null

# The exec agent of a workspace's toolchain containers (#136), tagged by stack like the runner.
AGENT_IMAGE ?= rotor-agent:$(STACK_PROJECT)

.PHONY: agent-image
agent-image: ## Build the workspace exec agent's image
	docker build -q -f $(ROOT)/deploy/Dockerfile.agent -t $(AGENT_IMAGE) $(ROOT) >/dev/null

# The smoke run gives the image what a sandbox gets: no network, no capabilities, a read-only root.
.PHONY: runner-smoke
runner-smoke: runner-image ## Run the image's self-check as a sandbox would
	docker run --rm --read-only --network none --cap-drop ALL --security-opt no-new-privileges $(RUNNER_IMAGE) --self-check

# The runner's tests that need the stack's tool double, run on the stack's network; test-integration calls this.
.PHONY: test-runner
test-runner: | $(UV_CACHE) ## Run the runner's tests against this checkout's running stack
	@mkdir -p $(REPORTS)
	$(call uv_run,--network $(STACK_NET) -e ROTOR_TEST_TOOLSTUB_INTERNAL_URL=http://tool-stub:8080 \
		-e ROTOR_TEST_TOOLSTUB_CONTROL_URL=http://tool-stub:8081 \
		-e ROTOR_TEST_OPENBAO_URL=http://openbao:8200 -e ROTOR_TEST_OPENBAO_ROOT_TOKEN='$(OPENBAO_ROOT_TOKEN)') \
		sh -c 'uv sync --locked --quiet && uv run --no-sync pytest -m stack --junitxml=/work/reports/pytest-stack.xml'

.PHONY: check-python
check-python: | $(UV_CACHE) ## The runner gate: lint, format, types, tests, then a smoke run of the image
	@mkdir -p $(REPORTS)
	$(DOCKER_UV) sh -c 'uv sync --locked --quiet \
		&& uv run --no-sync ruff check . \
		&& uv run --no-sync ruff format --check . \
		&& uv run --no-sync mypy src tests \
		&& uv run --no-sync pytest -m "not stack" --junitxml=/work/reports/pytest.xml'
	$(MAKE) --no-print-directory runner-smoke

endif
