# Security

## Reporting a vulnerability

Report it privately through
[GitHub's private vulnerability reporting](https://github.com/Cloudster1/Rotor/security/advisories/new),
never in a public issue or pull request. Say what is affected, how to
reproduce it, and what an attacker gains. You hear back within a week, and
are told when a fix is released and whether you want to be credited.

A problem in a sibling belongs to it:
[Exciter](https://github.com/Cloudster1/Exciter/security),
[Armature](https://github.com/Cloudster1/Armature/security) or
[Stator](https://github.com/praetorianer777/Stator/security); report it there.

## Supported versions

Fixes go into the latest release. Rotor is before 1.0.0, so upgrade to the
newest release to receive them.

## Development credentials

These are for development only and published on purpose; they are not a
vulnerability, and an installation never uses them:

- the Keycloak realm in `deploy/keycloak/realm.json`, its people's passwords
  `<name> password` and its client secrets `<client>-dev-secret`
- the development OpenBao's root token `rotor-dev-root`, and its single
  unseal key and first root token, which it keeps in `rotor-init.json` on its
  own volume so it can unseal itself on every start
- the development sandboxes' route to OpenBao, which they use only to unwrap
  the single-use wrapping tokens they are handed; they hold no credential
  there
- `ROTOR_SANDBOX_ALLOW_RUNC`, which lets a test cluster run sandboxes
  without gVisor; production refuses it
- the development worker's access to the host's Docker socket, which it uses
  to start sandboxes and which hands it the machine; the api no longer holds
  it, and the Docker sandbox backend refuses to run outside development
- the database and Valkey passwords defaulted in `deploy/docker-compose.yml`
  and `mk/stack.mk`
- the tool-stub's tokens, codes and client secrets (`stub_*`), which open
  nothing but the test double, and its personal tokens `stub_pat_<name>`
- the suite's Exciter and its bootstrap administrator `admin@exciter.test` /
  `exciter admin password`, Exciter's own development account, which
  `make tool-snapshots` signs in with
