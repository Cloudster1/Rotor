# API contract for M10: assistants (#47)

An assistant, such as Claude Code, works with Rotor over MCP as the person
who connected it: it lists and reads workflows and runs, starts and cancels
runs, and reads approvals. It never decides an approval. Rotor is the
authorization server of its own MCP endpoint, as Exciter is of its
(Exciter #188), with registration only (RFC 7591), the authorization code
flow with PKCE S256, and tokens for MCP alone.

Migration 01000 adds `assistant_setting`, `oauth_client`, `oauth_request`,
`oauth_grant`, `oauth_code` and `oauth_token`, the columns
`audit_log.via_grant` and `audit_log.via_client`, and `current_actor_via()`.

## Discovery, at the root

| Path | Answers |
|---|---|
| `GET /.well-known/oauth-protected-resource` | RFC 9728: `resource` is `{ROTOR_APP_URL}/api/v1/mcp`, `authorization_servers` is `[ROTOR_APP_URL]`, scopes `mcp` and `mcp:read` |
| `GET /.well-known/oauth-protected-resource/api/v1/mcp` | The same, where a 401 at MCP points |
| `GET /.well-known/oauth-authorization-server` | RFC 8414: the endpoints below, `S256`, `none`, `client_secret_basic` and `client_secret_post`, and the `iss` parameter (RFC 9207) |

All three answer 404 while administrators have switched assistants off. The
web client's nginx sends `/.well-known/oauth-` to the api.

## The protocol, under `/api/v1/oauth`

| Operation | What it does |
|---|---|
| `POST /register` | Registers an assistant: 1 to 10 redirects, each https or http on a loopback address at any port, no fragment. 201 with `client_id`, and `client_secret` (prefix `rotor_ocs_`) only when one was asked for. 10 an hour from one address, then 429 `temporarily_unavailable` |
| `GET /authorize` | Checks, in order: the client, the redirect, whether assistants are on, `response_type=code`, `state` with an S256 `code_challenge`, `resource` naming the MCP endpoint, and the scope. The first three refused go to `/oauth/consent?error=invalid_client`, `invalid_redirect` or `switched_off`, never to the redirect; the rest go back to it with `error`, `error_description`, `state` and `iss`. Otherwise the request is kept for 10 minutes and the browser goes to `/oauth/consent?request={id}` |
| `POST /token` | A form. `authorization_code` with its verifier and redirect, or `refresh_token`, which turns over and may only narrow to `mcp:read`. Access tokens (`rotor_oat_`) last an hour, refresh tokens (`rotor_ort_`) 30 days. A code or refresh token used twice ends its grant. Never cached |
| `POST /revoke` | RFC 7009: an access token ends alone, a refresh token with its grant; a token unknown answers 200 too |

## The consent page and the lists

These take a session and refuse an assistant's token, 403 `session_only`.

| Operation | What it does |
|---|---|
| `GET /oauth/requests/{requestID}` | `{request: {id, client: {id, name, uri}, redirectHost, readOnly, expiresAt}}` |
| `POST /oauth/requests/{requestID}` | `{allow, readOnly}`: answers `{redirectTo}`, the assistant's redirect with a code (`rotor_oac_`, 1 minute) or `access_denied`. A grant replaces the person's earlier grant to the same assistant. An assistant that asked to read is granted no more, 422 on `readOnly`. Audited `oauth.granted` |
| `GET /oauth/grants` | The caller's assistants, `{grants: [{id, client, redirectHost, readOnly, createdAt, lastUsedAt}]}`, last used first |
| `DELETE /oauth/grants/{grantID}` | Disconnects one; every token it holds stops at once. Audited `oauth.revoked`, `by: person` |
| `GET /oauth/grants/everyone` | Administrators: everybody's, each with its `owner` |
| `DELETE /oauth/grants/everyone/{grantID}` | Administrators: disconnects anybody's, `by: administrator` |
| `GET /settings/assistants`, `PUT /settings/assistants` | Administrators: `{connectionsOn, updatedAt}`. Off refuses every token at once and keeps the grants. Audited `oauth.switched` |

## `POST /api/v1/mcp`

One JSON-RPC 2.0 request, answered in JSON; batches are refused, a
notification answers 202. MCP 2025-11-25 and 2025-06-18. The methods are
`initialize`, `ping`, `tools/list` and `tools/call`; there are no resources
or prompts. Without a caller it answers 401 with
`WWW-Authenticate: Bearer resource_metadata="{ROTOR_APP_URL}/.well-known/oauth-protected-resource/api/v1/mcp", scope="mcp"`,
and `error="invalid_token"` when a credential was sent. A foreign `Origin` is
refused, 403 `cross_site`, whatever the credential.

An access token is accepted at MCP and for the calls its tools make, nowhere
else: sent to any other path it answers 401 `invalid_token`.

### The tools

Each is a row of the operations table, called through the whole router as
the HTTP call it stands for, so it is held to everything that call is. An
answer over 64 KiB is refused with a sentence on how to narrow it.

| Tool | Operation | Hints |
|---|---|---|
| `whoami` | `GET /auth/me`, whose answer names the `assistant` | read |
| `list_workflows` | `GET /workflows` | read |
| `get_workflow` | `GET /workflows/{workflowID}` | read |
| `validate_workflow` | `POST /workflows/validate` | read: it changes nothing |
| `start_run` | `POST /workflows/{workflowID}/runs`; `idempotencyKey` is the Idempotency-Key header | destructive, open world |
| `list_runs` | `GET /runs`, which takes `limit` (1 to 200) | read |
| `get_run` | `GET /runs/{runID}` | read |
| `cancel_run` | `POST /runs/{runID}/cancel` | destructive |
| `list_approvals` | `GET /approvals` | read |
| `get_approval` | `GET /approvals/{approvalID}` | read |

A grant made to read is offered the read tools alone, and a write it calls
anyway is refused in a sentence.

### What an assistant never does

Deciding an approval is no tool, and four layers hold it:

1. No row that decides carries a tool, which a unit test holds.
2. `POST /approvals/{approvalID}/decision` takes a session alone.
3. `approvals.Service.Decide` refuses a caller acting through an assistant,
   403 `decided_in_person`.
4. The database refuses an approval approved or rejected in a transaction
   whose `app.via` names a grant, `insufficient_privilege`.

Connecting or disconnecting a tool, consenting, ending a grant, signing out
and every administrator's operation take a session too.

## Client ID metadata documents (#114)

An assistant may name itself by the https address of its client ID metadata
document instead of registering. `/.well-known/oauth-authorization-server`
says `client_id_metadata_document_supported: true`. Migration 01010 adds
`oauth_client.kind` (`registered` or `metadata`) and `fresh_until`.

- **Which ids are documents.** A `client_id` that is an https address with a
  path, no fragment and no userinfo is fetched when Rotor does not know it,
  or when its kept copy went stale.
- **The fetch.** A GET with `Accept: application/json`, within 5 seconds, of
  at most 5 KiB, answering 200, with no redirect followed. It is kept as long
  as `Cache-Control: max-age` says, at most a day; `no-store` and `no-cache`
  keep it not at all.
- **The guard.** The fetch never reaches Rotor's own network:
  - loopback, private, link-local, carrier-grade NAT, the cloud metadata
    ranges, unspecified and multicast addresses are refused after the name
    is resolved, and the address checked is the one dialled;
  - `ROTOR_OAUTH_FETCH_ALLOW` (chart: `api.oauthFetchAllow`) names host
    names and CIDRs inside the network that may still be reached.
- **What a document must hold:**
  - its own address as `client_id`;
  - `token_endpoint_auth_method` absent or `none`, since a document keeps no
    secret;
  - the rules a registration is held to: redirects, grants, response types
    and name.
- **Refusals.** A document that cannot be fetched or is refused sends the
  browser to `/oauth/consent?error=invalid_client`, never to the assistant's
  redirect.
- **What the person sees.** The consent request and the grant lists carry
  `client.kind`, and the consent page says how Rotor knows the assistant.

## The audit log

Every entry written while an assistant acts carries `via`, the assistant's
name, stamped by the database from the grant; the API and the CSV export
name it.
