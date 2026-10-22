# API contract for M9: Rotor in the suite (#43, #45)

Rotor says what it offers and reads what each tool offers, by the capability
names of [`docs/integration.md`](integration.md):
- `GET /meta` lists `hooks@1`, `runs@1` and `workflows@1`
- a tool answers what Rotor needs and the tool does not list, under `missing`
- an administrator asks Rotor to read a tool again, for a tool that was updated

#43 needs no migration: the capabilities a tool listed are kept in
`tool.capabilities` since M1. #45 adds migration 01020 for Armature.

## `GET /meta`

`capabilities` is no longer empty: `["hooks@1", "runs@1", "workflows@1"]`,
sorted. The rest of the answer is unchanged.

## A tool's `missing`

Every tool answer (`GET /tools`, `GET /tools/{toolID}`, `POST /tools`, `POST
/tools/{toolID}/check`) carries `missing`: what Rotor needs that the tool did
not list when last asked, in the order of the table in `docs/integration.md`,
and an empty list for a tool that offers it all.

```json
"missing": [
  {
    "capability": "webhooks@1",
    "message": "Exciter does not offer webhooks@1, which Rotor needs to start workflows from its events. Update Exciter to a release that offers it, or ask its administrator to."
  }
]
```

- `mcp@1` missing: the Connections page offers no connecting and says what to
  update. Adding a tool without it is still refused (422 `tool_refused`).
- `webhooks@1` missing: `PUT /tools/{toolID}/hook` answers 422 `tool_lacks`
  with the sentence; a secret kept before stays, and deliveries keep being
  taken.

## `POST /tools/{toolID}/check`

Administrators only. Reads the tool's `/meta`, its protected resource metadata
and its authorization server's metadata again, and keeps its name, endpoints,
scopes and capabilities as they answer now. It never registers again, so every
person's connection stays.

| Status | When |
|---|---|
| 200 | `{tool}`, as it answers now; a tool that stopped offering `mcp@1` is kept, with it under `missing` |
| 404 | No such tool |
| 422 `tool_refused` | The tool answers, but its OAuth metadata no longer holds, in a sentence |
| 502 `tool_unreachable` | The tool did not answer |

The audit log records `tool.checked` with the tool's name, its capabilities and
what is missing.

## Armature (#45)

Migration 01020 adds `tool.mcp_url`, filled from `resource` for the tools kept
so far, `tool.grant_types` and the status `awaiting_approval`.

- **Every tool answer** carries `mcpURL`, where workflows call MCP, beside
  `resource`, what tokens are asked for: the same at Exciter, the API holding
  MCP (`{base}/api/v1`) at Armature.
- **`status: awaiting_approval`**: a client the tool holds pending until its
  administrator allows Rotor. People may connect, and the first connection
  makes it `ready`; the audit log records `tool.allowed`.
- **Adding a tool** finds the resource metadata where the MCP endpoint's 401
  points, else at the well-known path, else at the host's root. A tool whose
  grant holds one access token (Armature) and which offers no token exchange is
  refused (422 `tool_refused`).
- **Run tokens.** Where the authorization server lists
  `urn:ietf:params:oauth:grant-type:token-exchange`, Rotor registers for it,
  and each run's token is exchanged from the grant's for the binding's scopes,
  with the tool's own required scopes beside them, for
  `config.ExchangedTokenLifetime` (15 minutes). An exchange refused with
  `invalid_scope` is the run's scopes not consented; `unauthorized_client` marks
  the tool for registering again.
- **Against the real Armature (#130):** the suite runs Armature at
  `deploy/suite/ARMATURE_REF` (01f8d07, the first with `mcp@1`).
  `backend/test/suite_armature_test.go` adds it, has its administrator allow
  Rotor, connects alice and dave, and runs a comment as each: alice's is
  hers, its token answers `/auth/me` as her through Rotor until the run ends
  and then 401, her grant still mints, and dave's is refused.
  `suite_story_test.go` runs `examples/story-to-requirements.yaml` from
  Armature's own webhook to Exciter's requirements.
