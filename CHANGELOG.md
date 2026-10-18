# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the versioning [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- A development stack and a test gate (#1). `make up` starts Postgres,
  Valkey, Keycloak with the suite's realm `dynamo` and an OpenBao dev server
  configured for Rotor, on ports of the checkout's own; `./run-tests.sh` runs
  the same layers locally and in CI, and the realm gains Rotor's sign-in
  client, its control-plane service account and the `rotor-administrators`
  group.
