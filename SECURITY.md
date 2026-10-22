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

## What is in scope

[`docs/threat-model.md`](docs/threat-model.md) says what Rotor protects and
how; its open findings are known and tracked. These are the claims most worth
trying to break:

| Area | The claim to try to break |
|---|---|
| People's tools | A run acts in a tool as the person who started it, with no more than their rights and the binding's scopes, and a read binding never writes |
| Tokens and keys | Refresh tokens, provider keys and webhook secrets never leave OpenBao except into a run's memory, and never land in the database, a log, an event or a span |
| The sandbox | A workflow's code, or content a run reads, cannot reach another run, the control plane's secrets or a host outside the tools, OpenBao, Rotor and the provider |
| The runner protocol | A sandbox can act only for its own run, and its bootstrap and session tokens work once |
| Approvals | A run waiting for an approval continues only by a decision of the people the pinned workflow names |
| Triggers and hooks | Only a delivery signed with the tool's secret, within five minutes and seen once, starts a run, and only as an owner who may still read what it is about |
| Row-level security | A person sees only the workflows, runs, approvals and connections the contracts give them, also through SQL as `rotor_app` |
| The audit log | Nobody, administrators included, changes or empties it |

## What is not

- **The development stack** and its published credentials, listed below.
- **Anything requiring an administrator.** Administrators configure tools,
  providers, limits and LangSmith; they are the trust boundary, not a target
  inside it.
- **`/metrics`.** The api and the worker serve it without authentication on
  a port of their own, meant for the cluster network; the chart never
  exposes it through the ingress.
- **Denial of service by volume** from people who may sign in.
- **A model following planted text** on its own. Fencing makes it less
  likely; the claim to break is that it cannot then write through a read
  binding, skip an approval or reach a host outside the egress policy.

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
  `exciter admin password`, Exciter's own development account, which the
  suite's seed connects to the suite's Armature, and which `make
  tool-snapshots` signs in with at the throwaway Exciter it starts on ports
  nobody else reaches and removes with its data once it has recorded
- the suite's Armature and its administrator `admin@armature.test` /
  `armature admin password`, whom `make suite-up`'s seed sets it up as; the
  seed also lets alice, bob and dave in with their realm passwords, and points
  Armature at the realm's `armature` client and its development secret
- `snapshot@armature.test` / `armature snapshot password`, the first account
  of the throwaway Armature `make tool-snapshots TOOL=armature` starts on
  ports nobody else reaches and removes with its data once it has recorded
