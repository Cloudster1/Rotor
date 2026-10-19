# Threat model

This document says what Rotor protects, from whom, and how, component by
component. It builds on [`architecture.md`](architecture.md), which describes
the parts, and [`decisions.md`](decisions.md), which says why they are built
the way they are. Every threat below has a status:

- **Built**: the mitigation exists, with the issue or file that brought it.
- **Open #N**: not mitigated yet; the issue tracks it.
- **Accepted**: a risk Rotor takes on purpose, with the reason.

A change that moves a trust boundary, adds a component or closes an open
finding updates this document in the same pull request.

## What Rotor protects

| Asset | Where it lives | Why it matters |
|---|---|---|
| Refresh tokens of people's tool connections | OpenBao, `rotor/connections/{user}/{tool}` | Each acts as the person in a tool until revoked |
| Access tokens minted for a run | OpenBao `rotor/runs/{run}`, the runner's memory | Act as the person for minutes |
| Model provider keys | OpenBao `rotor/providers/{id}/key`, the runner's memory | Spend money; read prompts |
| LangSmith key, webhook secrets | OpenBao | Send data out; start workflows |
| Run data: inputs, events, checkpoints, outputs, approvals | Postgres | Hold what a run read from the tools, which may be confidential |
| Sessions | Postgres (SHA-256 of the cookie) | Act as a person in Rotor |
| Workflow definitions and triggers | Postgres | Decide what runs as whom, and when |
| The audit log | Postgres, append-only | Proves who did what |
| The tools' own data | Exciter, Armature, Stator | What a hostile run would read or change |

## Who attacks

| Actor | Starts with | Wants |
|---|---|---|
| Outsider | The web origin, the hook endpoint | A session, a run, data |
| Signed-in member | A session | Another person's runs, data or connections; administrator rights |
| Content author | Text in a tool the run reads: an issue, a requirement, a wiki page, a hook payload | To steer a run into writing or leaking what its person can reach |
| Hostile code node | Code running in a sandbox | Tokens, keys, the control plane, other runs |
| Compromised tool or provider | Answers to Rotor's calls | Tokens of other tools; data from the run |
| Network attacker | A position between components | Tokens in transit |
| Supply chain | A dependency or base image | Code in every sandbox or in the control plane |

Administrators are the trust boundary, not a target inside it: they already
configure tools, providers, prices, limits and LangSmith, and read the audit
log. Operators who hold the database, OpenBao or the cluster hold everything
Rotor holds.

## Trust boundaries

```
         (1) browser                         (6) LLM provider
              |                                      ^
              v                                      |
+-- control plane (trusted) ------------------+      |
|  web, api, worker                           |      |
|  Postgres, Valkey, OpenBao, Keycloak (2)    |      |
+---------------------------------------------+      |
       ^ (3) gRPC over TLS, dialled out              |
       |     with a single-use credential            |
+-- sandbox, one per run (untrusted) ---------+------+
|  runner, its code nodes (4)                 |
+---------------------------------------------+
       | (5) MCP over HTTPS as the person
       v
  Exciter, Armature, Stator (each judges every call)
       |
       +--(7) webhooks, signed, into the api's public endpoint
```

1. **Browser to web and api:** a person's session.
2. **Control plane to its stores:** Postgres under two non-superuser roles and
   row-level security, OpenBao under one policy.
3. **Sandbox to control plane:** the runner protocol. Everything a sandbox
   sends is treated as untrusted.
4. **Runner to code node:** a child process with an empty environment and
   limits, in the same sandbox.
5. **Sandbox to tools:** the person's narrow, short-lived tokens. The tool
   enforces the person's rights.
6. **Sandbox to provider:** prompts carrying tool content.
7. **Tools to api:** event deliveries that start runs.

## STRIDE by component

Spoofing, Tampering, Repudiation, Information disclosure, Denial of service
and Elevation of privilege. Only the categories that apply to a component are
listed.

### Browser and web

| | Threat | Mitigation | Status |
|---|---|---|---|
| S | A stolen session cookie | HttpOnly, `Secure` outside development, `SameSite=Lax`; only its hash is stored; it lives 12 hours | Built (#6) |
| T | Cross-site requests with the cookie | Same-site check and, in production, the origin check on every cookie-carried write | Built (#6) |
| I | Script injection from tool content shown in a run | React escapes text; no markdown or HTML is rendered from tool or model content; a strict CSP in nginx | Built (#5, #24) |
| I | A link in a run's timeline leading to a host a content author chose | The timeline links only to the run's tools' hosts; any other address stays text | Built (#40) |
| I | Clickjacking | `frame-ancestors 'none'`, `X-Frame-Options: DENY` | Built (#5) |

### api

| | Threat | Mitigation | Status |
|---|---|---|---|
| S | Signing in as someone else | OIDC code flow with PKCE and state against Keycloak; one subject per person | Built (#6) |
| S | A forged back-channel logout | The logout token is verified against the realm's keys, its `jti` is kept to refuse a replay | Built (#6) |
| S | Keycloak's "sign out" of a person leaves their other Rotor sessions alive | After any logout token Rotor reads the person's sign-out time from Keycloak and ends every older session | Built (#53) |
| T | Starting a run of a workflow one may not see, or with inputs that do not fit | Row-level security on `workflow`; inputs checked against the schema; the insert allowed only as `queued` | Built (#22) |
| R | Denying an act | The audit log, written in the act's own transaction, append-only for both roles | Built (#2, #36) |
| I | Reading another person's runs, approvals or connections | Row-level security as `rotor_app`; administrators see what the contracts say | Built (#14, #22, #31) |
| D | Large bodies | Every body is capped, hooks at 256 KiB | Built |
| D | Volume from signed-in people | Per-person concurrency and daily quotas bound runs; requests themselves are not rate-limited | Accepted: a self-hosted installation serves known people |
| E | A member reaching administrator routes | Role from the realm group, checked by the router and by row-level security | Built (#6) |
| E | Writes through a binding whose call a tool later marks as a write | `approval: beforeWrite` is checked when a workflow is saved, against the catalog the saver saw; a manual run does not check again | Open #90 |

### worker

| | Threat | Mitigation | Status |
|---|---|---|---|
| T | Two workers acting on one run | Leases with `FOR UPDATE SKIP LOCKED`; advisory locks for the scheduler | Built (#22, #34) |
| E | A compromised worker in a cluster | Its Role reaches pods and Secrets in the sandbox namespace only, which enforces the restricted Pod Security standard | Built (#26) |
| E | The development worker holds the host's Docker socket | The Docker backend refuses to run outside development | Accepted: development only (`SECURITY.md`) |

### Postgres

| | Threat | Mitigation | Status |
|---|---|---|---|
| T | Rewriting the audit log | `UPDATE`, `DELETE` and `TRUNCATE` refused to both roles | Built (#2, #36) |
| I | A database dump or backup showing tool content | Each run's inputs, output, events, checkpoints and runner messages are sealed with a data key of its own, wrapped by OpenBao transit and bound to its row | Built (#41) |
| I | Approval messages, previews and values in clear | Redacted, capped and sealed with their run's key, bound to the interrupt they answer | Built (#91) |
| I | Run data kept forever | Finished runs are deleted after the installation's retention, 90 days by default, or a workflow's shorter one; the audit log records each deletion and is never deleted | Built (#41) |
| E | The api running as a superuser | Refused at start-up outside development | Built (#3) |

### Valkey

| | Threat | Mitigation | Status |
|---|---|---|---|
| T | Forged pub/sub messages | A message only wakes a replica; events and cancels are read from Postgres | Built (#24) |
| T | A poisoned tool catalog cache, marking a writing call read-only | The runner checks `readOnlyHint` live for read bindings; saving trusts the cache (see #90) | Open #90 |
| I | Valkey reachable without a password or TLS | The chart and the api refuse a Valkey without a password outside development; `valkey.tls` connects over TLS | Built (#92) |
| D | Losing Valkey | It holds nothing that is not elsewhere; losing it costs latency | Built (#24) |

### OpenBao

| | Threat | Mitigation | Status |
|---|---|---|---|
| S | Another workload reading Rotor's secrets | JWT auth in development, Kubernetes auth in a cluster, one policy for the control plane | Built (#7, #26) |
| T | Changing the transit key's settings | The policy grants making data keys and decrypting them only | Built (#19, #41) |
| R | No record of who read a secret | Rotor's init script refuses an OpenBao without an audit device; the development stack declares one | Built (#92) |
| I | A sandbox reading other runs' tokens | A sandbox holds no OpenBao identity; it can only unwrap a wrapping token, once, within two minutes | Built (#23, #26) |
| D | A sealed OpenBao | Readiness fails, so the api leaves rotation | Built (#7) |

### Keycloak

| | Threat | Mitigation | Status |
|---|---|---|---|
| S | Keycloak is the single source of identity for the suite | Shared with the siblings; operated by the installation | Accepted |
| I | The `rotor` client's service account may read the realm's people (`view-users`), to see when a person was signed out | Its secret stays in the api; the control plane learns the same people as they sign in | Accepted (#53) |
| E | Group changes not reaching Rotor | The role is read from the groups at every sign-in; switching a person off ends their sessions and triggers | Built (#6, #34) |

### Runner and sandbox

| | Threat | Mitigation | Status |
|---|---|---|---|
| S | A sandbox posing as another run | The bootstrap token is single-use, bound to one run, and lasts 5 minutes; each reconnect uses up a session token | Built (#18) |
| T | A sandbox sending events, checkpoints or spans for another run | The stream is bound to its run; span trace ids must match the run's | Built (#18, #37) |
| T | A sandbox under-reporting its tokens | Usage is as honest as the runner | Accepted (decisions.md, #38) |
| T | A sandbox answering an approval it was not given | Who decides, the form and the expiry come from the pinned spec; the answer is keyed by the interrupt | Built (#31) |
| I | Tokens and keys in events, logs or spans | The runner cuts the secrets it holds and known credential shapes from events, output, spans and its log; the control plane does the same again and for its own log lines | Built (#41) |
| I | Leaking data to a host of the content author's choice | Egress only to the runner endpoint, OpenBao, the tools and the provider | Built (#27) |
| D | A run that never ends | Step, token, cost and time limits; `activeDeadlineSeconds`; up to three sandboxes per run | Built (#22, #26, #38) |
| E | Escaping the sandbox | gVisor, non-root, read-only, no capabilities, no ServiceAccount token, restricted Pod Security | Built (#21, #26) |

### Code node

| | Threat | Mitigation | Status |
|---|---|---|---|
| I | Reading the runner's environment | The child gets an empty environment; tokens never enter the environment | Built (#17) |
| I | Reading the runner's memory, which holds tokens and the provider key | The runner makes itself not dumpable before it reads its bootstrap token, so a child of the same user cannot trace it or open its memory, environment or files | Built (#89) |
| E | Calling tools outside its bindings | Tool calls go back to the runner, which applies the node's bindings | Built (#17) |
| D | Exhausting the sandbox | Memory, CPU, open files and a timeout per node | Built (#17) |

### Tools

| | Threat | Mitigation | Status |
|---|---|---|---|
| S | A tool impersonated on the way | HTTPS; the MCP endpoint and authorization server come from the tool's own metadata (RFC 9728, RFC 8414) | Built (#7) |
| S | A spoofed webhook delivery | HMAC-SHA256 of the timestamp and body with a secret from OpenBao | Built (#35) |
| I | A token for one tool reaching another | Each token is minted for one tool's resource and the binding's scopes | Built (#12) |
| E | A run doing more than its person may | The tool enforces the person's rights on every call; read bindings never write | Built (#12, #16) |
| E | Content in a tool steering a run | Fenced as data under a fixed policy, links limited, a warning on unapproved writes; see [the analysis](#untrusted-content-private-data-and-a-way-out) | Built (#40); write confirmation open #32 |

### LLM provider

| | Threat | Mitigation | Status |
|---|---|---|---|
| I | Prompts carry tool content to the provider | An administrator chooses the provider; that is the purpose | Accepted |
| T | A model answer steering a write | Read bindings, approvals; tool content fenced as data; links in what an agent writes limited to the tools' hosts | Built (#40) |

### Traces and LangSmith

| | Threat | Mitigation | Status |
|---|---|---|---|
| I | Spans carrying secrets | Attributes are redacted by the runner and again by the api | Built (#37) |
| I | Run data leaving the installation | LangSmith is off until an administrator turns it on and acknowledges it; the key stays in the control plane | Built (#37) |
| I | Data in Jaeger or LangSmith outliving Rotor's retention | The operator's collector and LangSmith keep their own retention | Accepted, documented with #41 |

### Images and dependencies

| | Threat | Mitigation | Status |
|---|---|---|---|
| T | A sandbox image that is not the one Rotor built | Images carry an SPDX SBOM and a cosign signature; the chart names every Rotor image by digest and, with policy-controller, refuses an unsigned sandbox image | Built (#42) |
| T | A vulnerable dependency | Versions pinned and at least two weeks old; govulncheck, pip-audit and npm audit nightly and on dependency changes; Renovate proposes updates | Built (#42) |
| T | A tampered image in the api, the worker or the web | The admission policy covers the sandbox namespace only | Accepted: the control plane's namespace is the operator's |

## Analyses

### Untrusted content, private data and a way out

A run combines three things: content a stranger may have written (an issue,
a wiki page, a hook payload), data only its person may read, and ways to send
data somewhere. Together they let a content author write instructions that a
model follows, read what the person can read, and send it to the author.

What stands in the way, from the outside in:
- **Ways out are few.** A sandbox reaches only the runner endpoint, OpenBao,
  the tools and the provider (#27). A link to an author's host is not a way
  out by itself, but it becomes one once a person clicks it, or once a tool
  shows it in a page that loads it.
- **Writes are declared.** A read binding never writes, whatever the prompt
  says, since the runner offers it only the calls a tool marks read-only
  (#16). Writing is opt-in per binding.
- **People decide writes.** An approval node stops a run before its writes,
  and a trigger refuses any write not behind one (#31, #34). Approvals are
  enforced outside the sandbox.
- **Content is data (#40).** Every agent works under a fixed system policy,
  and everything a prompt quotes, from tool answers, earlier steps and the
  run's inputs, sits inside a fence with an id random to the sandbox; a
  marker planted in content is defanged, so it cannot close one.
- **Links lead only to the tools (#40).** A link in an agent's answer, its
  streamed text or the arguments it sends a tool is removed unless its host
  is one of the run's tools', and a `link.removed` event says so; the run's
  page links only to those hosts.
- **Unwatched writes are named (#40).** Validation warns about a workflow
  that reads and then writes without an approval in between, and about an
  agent that reads and writes in one step; saving still works.
- **What is still to come:** `confirmWrites` holds each write for a person
  (#32), blocked on narrower tool tokens (Cloudster1/Exciter#235). A code
  node is the author's own code, so links it writes are not limited.

Fencing lowers the chance a model follows planted text; it does not prevent
it. The guarantees are the bindings, the approvals and the egress policy.

### Code-node escape

A code node is the workflow author's Python. Authors are trusted to write
workflows, not to read other people's tokens or other runs. Each layer
assumes the one inside it has failed:

1. **The child process:** an empty environment, resource limits, and JSON in
   and out. Its tool calls are judged by the runner (#17).
2. **The sandbox:** non-root, read-only, no capabilities, no ServiceAccount
   token, no OpenBao identity. It holds only its run's tokens, and those only
   for minutes (#21, #23, #26).
3. **gVisor:** system calls are served by a user-space kernel (#26).
4. **The network:** a default-deny policy, with hostnames allowed by Cilium
   (#27).

Inside the sandbox the child runs as the runner's own user, so the runner
makes itself not dumpable before it reads its bootstrap token: without
`CAP_SYS_PTRACE`, which no sandbox has, the child can neither trace it nor
open its memory, environment or files (#89). Escaping the sandbox gains a
node of the cluster, which gVisor and the restricted Pod Security standard
make hard.

### Token replay

| Token | Replay is refused because |
|---|---|
| Runner bootstrap token | It is single-use, bound to one run and expires after 5 minutes (`config.BootstrapTTL`) |
| Runner session token | Each reconnect uses one up and is handed the next, until an hour past the run's timeout |
| Wrapping tokens for tool tokens and keys | They open once, within 2 minutes (`config.WrapTTL`); a second opening fails |
| Tool access tokens | Short-lived, narrowed to the binding's scopes, and revoked at the tool when the run ends or waits (#28) |
| Refresh tokens | Never leave OpenBao; the tools rotate them at each use, and Rotor refreshes one connection at a time |
| Back-channel logout tokens | Their `jti` is kept; `iat` must be within 5 minutes |
| Webhook deliveries | The signed timestamp must be within 5 minutes and the event id unseen in 7 days |
| Session cookies | Only the hash is stored; ending a session at Keycloak or in Rotor deletes the row |

### Webhook spoofing

The hook endpoint is public, so anyone can post to it. A delivery starts runs
only when all of these hold:
- the HMAC-SHA256 of `<timestamp>.<body>` matches the secret an administrator
  set, which lives in OpenBao;
- the timestamp is within five minutes;
- the event id is new;
- the trigger's owner may still see the workflow and holds the connections it
  needs;
- the owner can read the event's subject in the tool, checked as them;
- every write sits behind an approval.

A leaked secret lets an outsider start the matching triggers with forged
payloads, as their owners. Those runs still act only within the owner's
rights and the approvals. Turning the secret over in the tool and pasting the
new one ends that.

## Open findings

| Id | Finding | Severity | Issue |
|---|---|---|---|
| F2 | `approval: beforeWrite` is checked on save against a cached catalog, never again when a run starts | Medium | #90 |
| | A steered model's writes wait for nobody without an approval node | High | #32 |
