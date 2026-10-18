# The runner protocol in proto/: Go code by buf with plugins installed in the Go container, Python code by
# grpcio-tools in the uv container, and a check that both are what the .proto says. Needs both toolchains.
ifneq ($(wildcard $(ROOT)/proto/rotor/runner/v1/runner.proto),)

BUF_VERSION            ?= v1.73.0
PROTOC_GEN_GO_VERSION  ?= v1.36.12
PROTOC_GEN_GRPC_VERSION ?= v1.6.2
PROTO_FILE := rotor/runner/v1/runner.proto
PY_PROTO_OUT := src
GO_PROTO_DIR := backend/internal/runnerpb
PY_PROTO_DIR := runner/src/rotor_runner/pb

# The tools are built once into the Go cache and reused; a different version is built anew.
PROTO_TOOLS = [ -x /cache/bin/buf ] && /cache/bin/buf --version | grep -qx '$(BUF_VERSION:v%=%)' \
	|| { GOBIN=/cache/bin go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION) \
		&& GOBIN=/cache/bin go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION) \
		&& GOBIN=/cache/bin go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GRPC_VERSION); }
DOCKER_BUF = $(call go_run,-e PATH=/cache/bin:/usr/local/go/bin:/usr/bin:/bin)

# The proto is mounted under rotor_runner/pb, so the generated modules import each other from inside the package.
PY_PROTOC = uv run --no-sync python -m grpc_tools.protoc -Irotor_runner/pb=../proto \
	--python_out=$(1) --grpc_python_out=$(1) --mypy_out=$(1) --mypy_grpc_out=$(1) rotor_runner/pb/$(PROTO_FILE)

.PHONY: proto
proto: ## Regenerate the runner protocol's Go and Python code from proto/
	@mkdir -p $(GO_CACHE) $(UV_CACHE)
	$(DOCKER_BUF) sh -c '$(PROTO_TOOLS) && cd .. && buf generate'
	$(DOCKER_UV) sh -c 'uv sync --locked --quiet && $(call PY_PROTOC,$(PY_PROTO_OUT))'
	@find $(ROOT)/$(PY_PROTO_DIR) -type d -exec sh -c 'touch "$$1/__init__.py"' _ {} \;

.PHONY: proto-check
proto-check: ## Fail when the generated protocol code differs from proto/, or buf lint refuses it
	@mkdir -p $(GO_CACHE) $(UV_CACHE)
	@$(DOCKER_BUF) sh -c '$(PROTO_TOOLS) && cd .. && buf lint && buf generate -o /tmp/gen \
		&& diff -ru $(GO_PROTO_DIR) /tmp/gen/$(GO_PROTO_DIR)' \
		|| { echo "The Go code of the runner protocol is out of date or buf lint failed. Run make proto and commit the result."; exit 1; }
	@$(DOCKER_UV) sh -c 'uv sync --locked --quiet && mkdir -p /tmp/gen && $(call PY_PROTOC,/tmp/gen) \
		&& diff -ru -x __init__.py -x __pycache__ $(PY_PROTO_OUT)/rotor_runner/pb /tmp/gen/rotor_runner/pb' \
		|| { echo "The Python code of the runner protocol is out of date. Run make proto and commit the result."; exit 1; }

# Against main's proto/ as this checkout's git has it, exported on the host so a worktree's git needs no mount.
PROTO_MAIN := $(ROOT)/.cache/proto-main
.PHONY: proto-breaking
proto-breaking: ## Fail when the runner protocol changes incompatibly against main
	@mkdir -p $(GO_CACHE) && rm -rf $(PROTO_MAIN) && mkdir -p $(PROTO_MAIN)
	@if git -C $(ROOT) cat-file -e origin/main:proto 2>/dev/null; then \
		git -C $(ROOT) archive origin/main proto | tar -x -C $(PROTO_MAIN) \
		&& $(DOCKER_BUF) sh -c '$(PROTO_TOOLS) && cd .. && buf breaking proto --against .cache/proto-main/proto'; \
	else echo "main has no proto/ yet, so there is nothing to break"; fi

endif
