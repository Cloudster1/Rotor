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
- the OpenBao dev server's root token `rotor-dev-root`, and the dev server
  itself, which keeps everything in memory
- the database and Valkey passwords defaulted in `deploy/docker-compose.yml`
  and `mk/stack.mk`
