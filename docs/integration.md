# Integration capabilities

`GET /api/v1/meta` lists, under `capabilities`, the integration features this
build offers, as stable names with a version, such as `runs@1`. A partner
decides from these names whether a feature is there, not from the version
number of the build. `/meta` answers anyone, so a partner can ask before it has
an account; the names promise features, not data.

```json
{
  "product": "rotor",
  "build": { "version": "0.1.0", "commit": "abc1234", "builtAt": "2026-10-19T09:00:00Z" },
  "apiVersion": "1",
  "capabilities": ["hooks@1", "mcp@1", "runs@1", "workflows@1"]
}
```

The list is sorted. Paths below are under `/api/v1`, spelled as in
[`api/openapi.json`](../api/openapi.json). Every operation takes a session
unless the document marks it public.

The names and what each promises belong to the suite, in Dynamo's
[`docs/compatibility.md`](https://github.com/praetorianer777/Dynamo/blob/main/docs/compatibility.md)
(Dynamo#4); this page says what Rotor does with them and what Dynamo needs to
run Rotor ([below](#what-dynamo-needs-to-run-rotor)).

## What Rotor offers

### `workflows@1`

Workflows defined as versioned specs: listed, read, written, compared, shared
and checked before saving. A workflow's spec is the YAML of
[`docs/workflows.md`](workflows.md), against `api/workflow.v1.schema.json`.

- `GET /workflows`, `POST /workflows`
- `GET /workflows/{workflowID}`, `PUT /workflows/{workflowID}`,
  `DELETE /workflows/{workflowID}`
- `GET /workflows/{workflowID}/versions`,
  `GET /workflows/{workflowID}/versions/{versionNumber}`,
  `GET /workflows/{workflowID}/diff`
- `PUT /workflows/{workflowID}/sharing`
- `POST /workflows/validate`

### `runs@1`

A workflow started as the caller, which acts in the tools with the caller's
own permissions; its runs listed and read, cancelled, and its events streamed
as they happen.

- `POST /workflows/{workflowID}/runs`
- `GET /runs`, `GET /runs/{runID}`
- `POST /runs/{runID}/cancel`
- `GET /runs/{runID}/events`, as server-sent events

### `hooks@1`

A tool's signed webhook deliveries, envelope version 1 or CloudEvents, received
for the event triggers they start ([`docs/triggers.md`](triggers.md)). An
administrator keeps the secret the tool made for Rotor.

- `POST /hooks/{toolID}`, public, checked by the tool's timestamped signature
- `GET /tools/{toolID}/hook`, `PUT /tools/{toolID}/hook`,
  `DELETE /tools/{toolID}/hook`

### `mcp@1`

The name has one promise, shared by Exciter, Armature and Rotor (Stator will,
praetorianer777/Stator#600): the tool serves its own operations as MCP tools
(MCP 2025-11-25 or 2025-06-18) at `POST /mcp`, behind its OAuth, so an
assistant works as the person whose token it holds. Which operations those are
is each tool's `tools/list`, not part of the name. Rotor's are below.

An assistant works with workflows, runs and approvals over MCP 2025-11-25 or
2025-06-18, as the person who connected it through Rotor's own OAuth
authorization server (RFC 9728, RFC 8414, PKCE S256); an assistant registers
(RFC 7591) or names its client ID metadata document (#114). Its tools are
`whoami`, `list_workflows`, `get_workflow`, `validate_workflow`, `start_run`,
`list_runs`, `get_run`, `cancel_run`, `list_approvals` and `get_approval`;
approvals are decided in Rotor's pages alone.
[`docs/api-contract-m10.md`](api-contract-m10.md) has the whole contract and
[`docs/assistants.md`](assistants.md) how to connect one.

- `POST /mcp`, whose 401 names the protected resource metadata

## What Rotor checks of the tools

Rotor reads a tool's `/meta` when an administrator adds it, registers it
again, or asks Rotor to check it again (`POST /tools/{toolID}/check`). It keeps
the list with the tool, and `GET /tools` answers what Rotor needs and the tool
did not list, under `missing`, each with the sentence that says what to update.
Checking again never registers again, so nobody's connection is lost.

| Capability | What goes without it |
|---|---|
| `mcp@1` | Everything: the tool cannot be added, and a tool that stops offering it shows on the Connections page as needing an update, with no way to connect |
| `webhooks@1` | Event triggers: Rotor takes no webhook secret for the tool, so none of its events start a workflow. A hook set before keeps working |

**Partner OAuth is read from the tool's metadata, not by name.** Connecting a
person needs the tool's protected resource metadata (RFC 9728) naming an
authorization server, whose metadata (RFC 8414) offers the authorization code
flow with PKCE S256. Rotor finds the resource metadata as an MCP client does:
where the MCP endpoint's 401 points (`resource_metadata`, on the tool's own
origin), else at the well-known path with the endpoint's path, else at the
host's root, and takes a resource that is the MCP endpoint or holds it. A tool
without them is refused when it is added, in a sentence that says which is
missing. Token exchange (RFC 8693) is read the same way, from the server's
`grant_types_supported`, for a tool that hands out one access token per
grant, such as Armature: where it is offered, Rotor registers for it and
every run's token is exchanged from the grant's. Exciter lists an exchange
too, but one its partner apps trade the identity provider's tokens by, which
its open registration never grants, so Rotor leaves it alone there (#139).

### Exciter

Exciter offers `mcp@1`, `webhooks@1` and `webhooks@2`, and its authorization
server registers Rotor by RFC 7591. Everything Rotor does works with it.
`deploy/suite/EXCITER_REF` names the commit Rotor is tested against, since
#139 one with test plans and executions: a workflow lists a project's test
plans (`list_test_plans`), starts an execution of one
(`start_test_execution`), records each case's result
(`record_test_result`) and closes it (`close_test_execution`); the three
write and ask for an approval as any write does, and reading a case's results
(`get_test_case_results`) does not. Test cases are requirements of a type
whose role is a test case (`list_types`), traced to what they verify with
`link_requirements`.

### Armature

Armature offers `webhooks@1`, partner OAuth (Armature#280), token exchange
and, since Armature#305 (Armature at 01f8d07), `mcp@1`, so an administrator
adds it by its address like Exciter (#45). Its issues, comments, transitions
and remote links are its MCP tools; `deploy/suite/ARMATURE_REF` names the
commit Rotor is tested against.

- **Allowing Rotor.** Rotor registers itself, and Armature holds the client
  until one of its administrators allows it: on the consent page, by
  connecting first, or under Settings, Connected apps. Until then the tool
  reads `awaiting_approval`; the first connection makes it `ready`. A client
  nobody allows within 24 hours is deleted, and the tool then needs
  registering again.
- **One token per grant.** A refresh replaces the grant's access token and
  revoking it ends the grant, so each run's token is exchanged from it, with
  the binding's scopes and `armature_expires_in`; revoking that ends only it.
  Without the exchange Rotor refuses to add Armature.
- **Scopes.** `read` with every other; `issue.write`, `issue.transition`,
  `comment.write` and `git.write` write; Rotor never asks for
  `webhooks.partner`.
- **As the person.** A run's write is the person's in Armature: a comment's
  author is them, and Armature refuses what their role does not allow, so a
  reader's run fails with Armature's refusal. A run's token answers
  `/auth/me` with the person and Rotor as its app; Armature does not mark the
  write itself with the app yet.
- **Events.** An Armature administrator adds a webhook at Rotor's hook for the
  tool (version 1); `docs/triggers.md` sets the story workflow up with it.
- **Repositories (#137).** Where Armature lists `git-proxy@1`
  (Armature#333), a workflow's repository binding names a repository its
  project connects, and a run clones and pushes it through Armature's git
  proxy, `{Armature}/api/v1/git/{project}/{owner}/{name}.git`, with the
  binding's own exchanged token as a bearer; a GitLab owner with subgroups is
  one segment, its slashes `%2F`. No host token reaches Rotor or OpenBao.
  - **Seeing it.** Saving checks a fixed repository with
    `list_repositories` as the person; one named by the run's inputs is
    checked when the run starts.
  - **Pushing.** A writing repository binding's token carries `git.write`,
    which people consent to when they connect, so a connection made before
    asks to connect again. Armature lets it push to a branch made for an issue
    the person may edit, by `create_issue_branch`, never to the default
    branch, a tag, a deletion or a force push. A reading binding's token never
    carries `git.write`.
  - **Branches and pull requests** are Armature's MCP tools
    (Armature#332): `create_issue_branch` and `open_pull_request` write as
    the person, with `issue.write`.
  - **Pinned at Armature main** with both (f0398bb). The suite does not clone
    through the real proxy yet; the tool double's git proxy stands in for it
    in the gate, held to the real git client.

### Stator

Stator offers `webhooks@1`, but neither `mcp@1` nor partner OAuth yet
(Stator#449). `mcp@1` is coming (praetorianer777/Stator#600); until it ships
Rotor cannot add Stator (#105).

## Tested against the siblings

- **The pinned commits, in every pull request.** `api/tools/<tool>/openapi.json`
  holds each sibling's document at the commit its `SOURCE` names (`make
  tool-contracts`). The tool-stub is held to Exciter's and Armature's, and to
  their `tools/list` recorded at the same commits (`make tool-snapshots`,
  at `deploy/suite/EXCITER_REF` and `ARMATURE_REF`, each from a throwaway
  installation that is gone again once it has recorded),
  and a unit test holds what Rotor reads of each tool to all three.
- **The real ones, in the suite.** `make suite-up` runs Exciter and Armature
  at their pinned commits beside the stack, on Rotor's Keycloak, and `make
  test-suite` connects them as a person would: Exciter's runs and events, and
  Armature's approval, its runs as alice and dave, and the story workflow
  from an Armature story to reviewed Exciter requirements (#45, #130). CI runs
  it nightly and on a pull request labelled `suite`.
- **Their main, every night.** `suite.yml` vendors each sibling's `main` into
  the working copy (`make tool-contracts-main`), runs the same tests (`make
  test-contracts`), then runs the suite against Exciter's and Armature's
  `main` (`EXCITER_REF=main ARMATURE_REF=main`) beside the pinned leg. A change that would break Rotor
  shows before it is tagged. Nothing it fetches is committed.

## The rules

- **A name is promised for what it lists.** The operations keep their paths,
  their parameters and the fields they answer with; a field may be added, none
  is taken away or changes meaning.
- **A breaking change raises the number.** Renaming or removing one of the
  operations above, a field a partner reads, or a meaning, makes the
  capability `runs@2`. The old name is still reported beside the new one, and
  still kept, for at least one Dynamo release.
- **The list is shared.** The names and what each promises are one list in
  Dynamo's `docs/compatibility.md` (Dynamo#4). A tool may only report names
  from it, and a new name is added there before any tool reports it; this page
  follows it.
- **The build is held to it.** The list lives in
  `backend/internal/httpapi/capabilities.go`. A unit test refuses a capability
  naming an operation the operations table does not have, or one only agreed
  and not built, and another refuses a name that is not `name@version` or is
  listed twice. What Rotor needs of the tools lives in
  `backend/internal/tools/needs.go`.

## What Dynamo needs to run Rotor

Rotor is not one of the three tools Dynamo pins in `versions.yaml`; it has not
released yet (#167). This is what Dynamo needs to know to install and size it.

**Who may sign in.** Rotor signs people in through the shared realm `dynamo`.
Everyone in that realm may sign in, and membership of the realm group
`rotor-administrators` makes an administrator. That realm membership is all the
access Rotor has until #161 adds an allow-list.

**Images and memory.** The compose stack (`make up`) runs eight images:
`rotor-backend` (the api, the worker, the migrate job and the tool stub),
`rotor-web`, `rotor-runner`, `rotor-agent` (the workspace exec agent, started
with a run, not a service), and Postgres 18, Valkey 8, OpenBao 2.7.1 and
Keycloak 26.2. `docker stats --no-stream` after `make up` had settled showed
about 810 MiB across its nine running containers, Keycloak 590 MiB of it, the
second OpenBao (`openbao-sealed`, for the sealed-start test) 75 MiB and
Postgres 74 MiB; the api and the worker each under 10 MiB until a run starts.
Allow 1 GiB for the stack, and for the gate's own builds more.

The chart (`deploy/charts/rotor/values.yaml`) installs the backend image twice
over (api, worker) and the web image, plus the migrate, roles, secrets and
OpenBao hook Jobs, which run before an upgrade and then end. At its defaults
(2 api, 1 worker, 2 web) it requests 384 MiB and is limited to 1536 MiB:

| Part | Replicas | Requests | Limits |
|---|---|---|---|
| api | 2 | 128 MiB, 100m | 512 MiB |
| worker | 1 | 64 MiB, 50m | 256 MiB |
| web | 2 | 32 MiB, 20m | 128 MiB |

The trial values (`values-trial.yaml`) run one api and one web beside a bundled
Postgres (256 MiB requested, 1 GiB limit) and Valkey (32 MiB, 256 MiB):
512 MiB requested, 2176 MiB limited. Postgres, Valkey, OpenBao and Keycloak are
otherwise the installer's. Sandboxes are counted apart: each run is a pod, and
a run with workspace steps adds one toolchain container per image that
defaults to 3072 MiB and 2000m (`sandbox.workspaces.toolchain`, #136), so size
the sandbox namespace for the runs allowed at once, not for the chart.

**What an umbrella chart sets.** Dynamo's chart sets, for Rotor:

- `openbao.addr`, the OpenBao Rotor shares or is given, and `openbao.configure.enabled`
  with `openbao.configure.tokenSecret.name` (key `token`) naming a Secret that
  holds a token allowed to configure OpenBao
- the OpenBao that token belongs to needs an audit device declared in its own
  server configuration (the init script refuses one without, and its token must
  be allowed `sudo` on `sys/audit`), and, when it runs in the cluster, the
  ClusterRole `system:auth-delegator` on its ServiceAccount so it can review the
  api's and the worker's tokens ([`docs/deployment.md`](deployment.md))
- `ingress.host`, which the session cookie and the same-site check derive from,
  with `oidc.issuer` and the Secret `oidc.clientSecret`
- `sandbox.egress.openbao` (`namespace` and `podLabels`, or `cidr`), so
  sandboxes reach OpenBao, and `sandbox.egress.hosts` with `cilium: true`, or
  `cidrs`, for the tools
- the gVisor RuntimeClass `sandbox.runtimeClass` (`gvisor` by default), which
  the cluster must have; without it sandboxes do not start unless
  `sandbox.allowRunc` says so, which production refuses

**Sandboxes in a compose server.** The Docker sandbox backend
(`ROTOR_SANDBOX_BACKEND=docker`) hands the worker the host's Docker daemon, so
it is for development only and the configuration refuses it outside
`development`. A server that runs Rotor from compose therefore runs it
without sandboxes, `ROTOR_SANDBOX_BACKEND` empty. The api then still answers a
run start with 201 and the run `queued`, since it needs OpenBao and not a
backend; the worker, which would carry the run, does not start and exits with
"ROTOR_SANDBOX_BACKEND is not set, so the worker has nowhere to start
sandboxes; set it to kubernetes, or docker in development". The person starting
the run is told nothing: the run stays queued.
