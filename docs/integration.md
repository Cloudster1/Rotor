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
`grant_types_supported`: where it is offered, Rotor registers for it and
every run's token is exchanged from the grant's.

### Exciter

Exciter offers `mcp@1`, `webhooks@1` and `webhooks@2`, and its authorization
server registers Rotor by RFC 7591. Everything Rotor does works with it.

### Armature

Armature offers `webhooks@1`, partner OAuth (Armature#280) and token
exchange, and `mcp@1` once Armature#305 ships (#45).

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
- **Scopes.** `read` with every other; `issue.write`, `issue.transition` and
  `comment.write` write; Rotor never asks for `webhooks.partner`.

### Stator

Stator offers `webhooks@1`, but neither `mcp@1` nor partner OAuth yet
(Stator#449), so Rotor cannot add it (#105).

## Tested against the siblings

- **The pinned commits, in every pull request.** `api/tools/<tool>/openapi.json`
  holds each sibling's document at the commit its `SOURCE` names (`make
  tool-contracts`). The tool-stub is held to Exciter's, and a unit test holds
  what Rotor reads of each tool to all three.
- **Their main, every night.** `suite.yml` vendors each sibling's `main` into
  the working copy (`make tool-contracts-main`), runs the same tests (`make
  test-contracts`), then runs the suite against Exciter's `main`
  (`EXCITER_REF=main`) beside the pinned leg. A change that would break Rotor
  shows before it is tagged. Nothing it fetches is committed.

## The rules

- **A name is promised for what it lists.** The operations keep their paths,
  their parameters and the fields they answer with; a field may be added, none
  is taken away or changes meaning.
- **A breaking change raises the number.** Renaming or removing one of the
  operations above, a field a partner reads, or a meaning, makes the
  capability `runs@2`. The old name is still reported beside the new one, and
  still kept, for at least one Dynamo release.
- **The list is shared.** The names and what each promises belong in one list
  in the Dynamo repository (Dynamo#4, not written yet). A tool may only report
  names from it; this page follows it once it is there.
- **The build is held to it.** The list lives in
  `backend/internal/httpapi/capabilities.go`. A unit test refuses a capability
  naming an operation the operations table does not have, or one only agreed
  and not built, and another refuses a name that is not `name@version` or is
  listed twice. What Rotor needs of the tools lives in
  `backend/internal/tools/needs.go`.
