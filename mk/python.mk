# The Python runner, run in uv's pinned image as the invoking user with the uv cache under .cache/.
# Defined only once runner/pyproject.toml exists, so run-tests.sh skips check-python before then.
ifneq ($(wildcard $(ROOT)/runner/pyproject.toml),)

UV_IMAGE ?= ghcr.io/astral-sh/uv:0.9.30-python3.13-bookworm-slim@sha256:531f855bda2c73cd6ef67d56b733b357cea384185b3022bd09f05e002cd144ca
UV_CACHE := $(ROOT)/.cache/uv
RUNNER_IMAGE ?= rotor-runner:dev

# The image's own Python, so nothing is downloaded at run time; copies, since the cache sits on another mount.
DOCKER_UV = docker run --rm \
	-u $(UID_GID) \
	-v $(ROOT):/work \
	-v $(UV_CACHE):/cache \
	-e HOME=/tmp \
	-e UV_CACHE_DIR=/cache \
	-e UV_PYTHON_DOWNLOADS=never \
	-e UV_LINK_MODE=copy \
	-w /work/runner $(UV_IMAGE)

$(UV_CACHE):
	@mkdir -p $@

.PHONY: uv-lock
uv-lock: | $(UV_CACHE) ## Update runner/uv.lock after changing runner/pyproject.toml
	$(DOCKER_UV) uv lock

.PHONY: py-fmt
py-fmt: | $(UV_CACHE) ## Format the runner and apply ruff's safe fixes in place
	$(DOCKER_UV) sh -c 'uv sync --locked --quiet && uv run --no-sync ruff format . && uv run --no-sync ruff check --fix .'

.PHONY: runner-image
runner-image: ## Build the runner's sandbox image
	docker build -q -f $(ROOT)/deploy/Dockerfile.runner -t $(RUNNER_IMAGE) $(ROOT) >/dev/null

# The smoke run gives the image what a sandbox gets: no network, no capabilities, a read-only root.
.PHONY: runner-smoke
runner-smoke: runner-image ## Run the image's self-check as a sandbox would
	docker run --rm --read-only --network none --cap-drop ALL --security-opt no-new-privileges $(RUNNER_IMAGE) --self-check

.PHONY: check-python
check-python: | $(UV_CACHE) ## The runner gate: lint, format, types, tests, then a smoke run of the image
	@mkdir -p $(REPORTS)
	$(DOCKER_UV) sh -c 'uv sync --locked --quiet \
		&& uv run --no-sync ruff check . \
		&& uv run --no-sync ruff format --check . \
		&& uv run --no-sync mypy src tests \
		&& uv run --no-sync pytest --junitxml=/work/reports/pytest.xml'
	$(MAKE) --no-print-directory runner-smoke

endif
