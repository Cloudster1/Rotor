# API contract for M9: Rotor in the suite (#43)

Rotor says what it offers and reads what each tool offers, by the capability
names of [`docs/integration.md`](integration.md):
- `GET /meta` lists `hooks@1`, `runs@1` and `workflows@1`
- a tool answers what Rotor needs and the tool does not list, under `missing`
- an administrator asks Rotor to read a tool again, for a tool that was updated

No migration: the capabilities a tool listed are kept in `tool.capabilities`
since M1.

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
