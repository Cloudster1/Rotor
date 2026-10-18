# API contract for M1: tool connections

M1 lets Rotor act in the tools as a person:
- an administrator adds a tool by its address, and Rotor registers itself
  there as an OAuth client
- a person connects their account in a tool once, on the tool's own consent
  page, and can see and end that connection
- every long-lived secret, a refresh token or a client secret, lives in
  OpenBao and nowhere else
- the control plane mints a short-lived access token per run, never more
  than the person consented to

This file is #7. The route table in `backend/internal/httpapi/operations.go`
and `operations_m1.go` is the source of truth for paths, fields and types,
and this file says what they mean. Operations it adds carry `pending: true`
and answer 501 until their issue is built.

All paths are under `/api/v1`. "signed in" is any person with a session;
"admin" is an administrator of Rotor (`rotor-administrators` in the realm),
and an operation for admins alone carries `admin: true` and refuses anyone
else with 403, before it answers 501. A tool the caller asks for that does
not exist is 404. A refusal is a sentence that says what to do.

## Migrations, packages and settings

- Migration ranges, in build order:
  - 00100 to 00119 for the tool registry (#9)
  - 00120 to 00139 for connections (#11)
  - 00140 to 00149 for minting (#12), if it needs a table at all
  - #8 and #10 need none
- Packages:
  - `internal/vault` (#10): the OpenBao client, using
    `github.com/openbao/openbao/api/v2`. Nothing outside it builds a secret's
    path.
  - `internal/tools` (#9): discovery, registration and removal.
  - `internal/connections` (#11, #12): the authorization code flow,
    disconnecting, and `Mint`.
  - `cmd/tool-stub` and `internal/toolstub` (#8): the test double for a tool,
    in the backend module so it shares its toolchain and image.
- Settings, all `ROTOR_` variables, required outside development:
  - `ROTOR_OPENBAO_ADDR`, `ROTOR_OPENBAO_ROLE` (default
    `rotor-control-plane`), `ROTOR_OPENBAO_MOUNT` (default `rotor`) (#10)
  - `ROTOR_CONTROL_PLANE_CLIENT_ID` and `ROTOR_CONTROL_PLANE_CLIENT_SECRET`:
    the realm client whose token OpenBao trusts (#10)
  - `ROTOR_TOOLS_BACKCHANNEL`: `public=reachable` pairs, as
    `ROTOR_OIDC_BACKCHANNEL`, for tools whose public address the api cannot
    reach (#8)
- Limits and lifetimes, in `internal/config`:
  - `ConnectionLoginTTL` = 10 minutes: a started connection the person has
    not finished consenting to.
  - `ToolTimeout` = 10 seconds: one call to a tool's metadata, registration,
    token or revocation endpoint.
  - `VaultTimeout` = 5 seconds: one call to OpenBao.
  - `MaxToolDocumentBytes` = 64 KiB: one metadata document or token answer.
- **Decided:** Exciter is the only tool that can be connected in M1, and the
  test double stands in for every tool in the gate (`docs/decisions.md`,
  2026-10-18).

## OpenBao layout (#10)

| Path (KV v2 at `rotor/`) | Holds | Written by |
|---|---|---|
| `connections/{userID}/{toolID}` | `refresh_token`, `scope`, `client_id`, `obtained_at` | connecting (#11), minting (#12) |
| `tools/{toolID}/client` | `client_id`, `client_secret`, `auth_method`, `registered_at` | adding a tool (#9) |

- The mount requires check-and-set on every write (`cas_required=true`) and
  keeps three versions. A writer names the version it read; a first write
  names 0.
- The api logs in with JWT auth, role `rotor-control-plane`, presenting a
  client-credentials token of the realm client `rotor-control-plane` whose
  audience is `openbao`. Its token lives an hour, is renewed, and the api
  logs in again when renewal ends.
- Policy `rotor-control-plane` (`deploy/openbao/policies/`) allows create,
  read, update and delete on `rotor/data/connections/*` and
  `rotor/data/tools/*`, read, list and delete on their metadata, and nothing
  else: no transit, no `sys`. Sandboxes get their own role in M4.
- `/readyz` answers 503 while OpenBao is sealed, uninitialized or
  unreachable.

## #8 The test double and the suite

- `tool-stub` serves any number of tools side by side, each under
  `/t/{tenant}`, so parallel tests never share clients or grants. A tenant is
  created on the fly by `POST /control/tenants`, reachable only inside the
  stack's network.
- Each tenant behaves as Exciter does: RFC 9728 and RFC 8414 metadata, RFC
  7591 registration, an authorize endpoint that insists on `state`, PKCE
  S256 and `resource` and answers with `iss` (RFC 9207), a consent page,
  token and revocation endpoints, rotating refresh tokens whose reuse ends
  the grant, `POST /api/v1/mcp` and `GET /api/v1/meta`. A tenant created
  without OAuth answers only the last two.
- Contract tests hold the stub to the siblings' vendored `api/openapi.json`
  and to a snapshot of Exciter's `tools/list`.
- The suite profile runs the real Exciter, built from a pinned commit, beside
  Rotor's stack and realm. Its tests run in a CI job of their own, nightly
  and on pull requests labelled `suite`; they are not a required check.

## #9 Tools

| Operation | Needs | Answers |
|---|---|---|
| `GET /tools` | signed in | `{tools}` |
| `POST /tools` | admin | `{baseURL, clientID?, clientSecret?}`; 201 `{tool}`, 200 when registering a known tool again, 422 a sentence naming what the tool lacks, 502 the tool did not answer |
| `GET /tools/{toolID}` | signed in | `{tool}` |
| `DELETE /tools/{toolID}` | admin | 204 |

- **Discovery.** From the base URL an admin enters, Rotor reads, in order:
  1. `GET {base}/api/v1/meta`, which must list `mcp@1`. The product it names
     becomes the tool's name.
  2. `GET {base}/.well-known/oauth-protected-resource/api/v1/mcp` (RFC 9728),
     whose `resource` becomes the tool's resource and whose first
     `authorization_servers` entry its issuer.
  3. `GET {issuer}/.well-known/oauth-authorization-server` (RFC 8414), whose
     `issuer` must equal the issuer exactly, and whose
     `code_challenge_methods_supported` must include `S256`.
- **Refusals**, each a 422 whose message names what is missing: no `mcp@1`,
  no protected resource metadata, no authorization server, an issuer that
  does not match, no S256, no registration endpoint and no client given, a
  registration the tool refused. The base URL is `https`, or `http` on a
  loopback address or in development.
- **Registering.** Rotor registers by RFC 7591 as a confidential client:
  `client_name` "Rotor", `token_endpoint_auth_method` `client_secret_basic`,
  `grant_types` authorization code and refresh token, one redirect URI,
  `{ROTOR_APP_URL}/api/v1/connections/callback`. An admin may instead give a
  client registered by hand. The client secret goes to OpenBao; Postgres
  keeps the client id and how it was registered.
- **Registering again.** A tool may forget a client (Exciter sweeps a client
  no grant has used for 24 hours, and an idle one after 30 days). An
  `invalid_client` from the tool marks it `needs_registration`; `POST /tools`
  with its base URL registers again and answers 200. Connections made with
  the old client must be made again.
- **Removing** ends every connection to the tool as #11 does, revoking at
  the tool where it still answers, then deletes the client secret and the
  tool.
- **Who sees what.** Every signed-in person sees every tool; only admins add
  or remove one, and the database refuses `rotor_app` an insert, update or
  delete by anyone else.
- **Audit:** `tool.added` (base URL, name, registration) and `tool.removed`
  (base URL, connections ended).

## #10 Secrets

No operations. `internal/vault` offers, for a person and a tool or for a tool
alone: read with its version, write with check-and-set, delete. A write whose
version is stale fails with `vault.ErrConflict`; a missing secret is
`vault.ErrNotFound`. Secrets never reach a log line, the audit log, a
response or the database.

## #11 Connections

| Operation | Needs | Answers |
|---|---|---|
| `GET /connections` | signed in | `{connections}`: the caller's own |
| `POST /tools/{toolID}/connection` | signed in | `{authorizeURL}`; 409 while the tool needs registering again, 502 the tool did not answer |
| `GET /connections/callback` | the session cookie | 302 to `/connections?connected={toolID}` or `/connections?error={code}` |
| `DELETE /tools/{toolID}/connection` | signed in | 204; 404 when the caller has no connection to the tool |

- **Connecting.** The web posts, then sends the browser to `authorizeURL`:
  the tool's authorization endpoint with `response_type=code`, the client
  id, the redirect URI, a fresh `state`, PKCE S256 and `resource` naming the
  tool's MCP endpoint (RFC 8707). `scope` is every scope the tool supports
  but those Rotor never uses (Exciter's `evidence`). The state, the verifier
  and the session it was started from are kept for `ConnectionLoginTTL`.
- **Returning.** The callback refuses a state that is unknown, expired, used
  or started by another session, and an `iss` that is not the tool's issuer
  (RFC 9207, against mix-up). It exchanges the code with HTTP Basic client
  authentication, keeps the refresh token in OpenBao and the granted
  `scope` in Postgres, and drops the access token unused.
- **Error codes** on the redirect: `denied` (the person said no at the tool),
  `expired` (the state is unknown or too old), `mismatch` (another session or
  another issuer), `tool` (the tool refused the exchange), `unavailable`
  (the tool or OpenBao did not answer).
- **Connecting again** replaces the connection: the new refresh token is
  written over the old with check-and-set. Exciter ends the earlier grant
  itself.
- **Disconnecting** revokes the refresh token at the tool (RFC 7009), which
  at Exciter ends the grant, then deletes the secret and the connection. It
  succeeds even when the tool does not answer, and says so in the audit.
- **Connect again.** An `invalid_grant` from the tool, or a secret OpenBao no
  longer has, marks the connection `reconnect` and deletes what is left of
  the secret.
- **Switching a person off** ends their connections too.
- **Who sees what.** A person sees and ends only their own connections; the
  database refuses `rotor_app` anyone else's.
- **Audit:** `connection.granted` (tool, scopes) and `connection.ended`
  (tool, `by`: `self`, `tool_removed`, `switched_off` or `tool` for an
  `invalid_grant`, and whether the tool confirmed the revocation).

## #12 Minting

No operations; the control plane calls it in process when a run starts (M2).

```go
type MintRequest struct {
	UserID, ToolID uuid.UUID
	Scopes         []string // what the run needs; within the consent
	RunID          string   // for the audit; empty until runs exist
}

type Token struct {
	Value          string // never stored, never logged
	ExpiresAt      time.Time
	Scopes         []string // what the token carries at the tool
	NarrowedAtTool bool     // false when the runner must enforce Scopes itself
}

func (s *Service) Mint(ctx context.Context, req MintRequest) (Token, error)
func (s *Service) Revoke(ctx context.Context, toolID uuid.UUID, token string) error
```

- **Refusals:** `ErrNotConnected`; `ErrScopeNotConsented`, naming the scopes
  outside the consent; `ErrReconnect` when the tool ended the grant.
- **One refresh at a time per connection**, under a Postgres advisory lock on
  the connection, held from reading the refresh token until the rotated one
  is written to OpenBao with check-and-set. Only then is the access token
  returned. A check-and-set conflict means the person connected again
  meanwhile: the token just obtained belongs to an ended grant, so Mint
  reads the new secret and tries once more.
- **A refresh never sends `scope`.** At Exciter a narrower scope on a refresh
  narrows the grant for good. Until a tool offers token exchange (RFC 8693,
  Cloudster1/Exciter#235), the token carries the consented scope,
  `NarrowedAtTool` is false, and the runner enforces the narrower scope.
- **Each call mints its own access token**, never cached or shared, so
  revoking one run's token touches no other run.
- **Audit:** `token.minted` (tool, scopes asked, scopes carried, run,
  expiry, `narrowed_at_tool`), never the token.

## What is left for later

- Narrowing at the tool by token exchange, once Exciter#235 ships, and the
  runner's enforcement of the narrower scope (M2).
- Armature and Stator, once they are authorization servers
  (Cloudster1/Armature#280, praetorianer777/Stator#449, Rotor#45).
- Kubernetes auth to OpenBao for sandboxes (M4).
- Guarding the api against addresses an admin enters that point inside the
  network (M8).
