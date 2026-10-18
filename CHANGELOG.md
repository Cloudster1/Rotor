# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the versioning [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Rotor has a face (#5): `make up` now serves the web client, a shell with
  Workflows, Runs, Approvals, Connections and Settings, each saying what will
  live there, in the suite's light and dark colours. Settings names the build
  the server runs. A browser suite checks every page with axe in both schemes.
- The workflow runner has a home (#4): `runner/` is a Python 3.13 project
  with LangGraph, langchain-core, langchain-mcp-adapters and grpcio pinned
  by `uv.lock`, checked by ruff, mypy and pytest, and built into a
  distroless, non-root image that proves itself with no network, no
  capabilities and a read-only root.
- The backend answers (#3): `make up` now migrates the database and starts
  the api, which serves `/healthz`, `/readyz`, `/api/v1/openapi.json` and
  `/api/v1/meta`. One route table drives the router and `api/openapi.json`;
  sign-in's routes are agreed and answer 501 until #6. The audit log is
  append-only and readable by administrators alone, which the database
  itself enforces.
- The architecture and the decisions behind it are written down (#2):
  a Go control plane and a Python LangGraph runner, Rotor as an OAuth
  client of each tool with refresh tokens kept in OpenBao, one sandbox per
  run that is destroyed when idle, and workflows as versioned YAML. See
  `docs/architecture.md` and `docs/decisions.md`.
- A development stack and a test gate (#1). `make up` starts Postgres,
  Valkey, Keycloak with the suite's realm `dynamo` and an OpenBao dev server
  configured for Rotor, on ports of the checkout's own; `./run-tests.sh` runs
  the same layers locally and in CI, and the realm gains Rotor's sign-in
  client, its control-plane service account and the `rotor-administrators`
  group.
