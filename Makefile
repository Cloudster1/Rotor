# Each area brings its own targets in mk/<area>.mk; run-tests.sh calls a gate target only when it exists.
.DEFAULT_GOAL := help

ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
VERSION := $(shell cat $(ROOT)/VERSION)
# Compose passes both to the image builds, so GET /api/v1/meta names the build a checkout made.
export ROTOR_BUILD_VERSION := $(shell $(ROOT)/scripts/build-stamp.sh version)
export ROTOR_BUILD_COMMIT := $(shell $(ROOT)/scripts/build-stamp.sh commit)
UID_GID := $(shell id -u):$(shell id -g)
REPORTS := $(ROOT)/reports
NODE_IMAGE ?= node:26-alpine

include $(wildcard $(ROOT)/mk/*.mk)

.PHONY: test-all
test-all: ## The whole gate, exactly as CI and the push hook run it
	$(ROOT)/run-tests.sh

.PHONY: test-shell
test-shell: ## The shell layer: stack, CI, port and branch-guard tests
	$(ROOT)/run-tests.sh shell

.PHONY: help
help:
	@grep -hE '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | sed 's/:.*## /\t/' | sort
