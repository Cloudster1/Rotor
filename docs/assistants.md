# Connecting an assistant

An assistant that speaks MCP, such as Claude Code, works with Rotor as you:
it finds your workflows, starts runs with your permissions in the tools,
follows them to their end, and reads the approvals a run waits at. It never
decides an approval; you do that in Rotor. What it may do is in
[`docs/api-contract-m10.md`](api-contract-m10.md).

## Claude Code

Give it Rotor's MCP endpoint, under the address you open Rotor at:

```sh
claude mcp add --transport http rotor https://rotor.example.com/api/v1/mcp
```

The first time it calls Rotor, it registers itself and opens Rotor's consent
page in your browser. Sign in if asked, then allow it. Tick **Only read** to
let it list and read without starting or cancelling runs.

Against the development stack the address is
`http://localhost:<WEB_PORT>/api/v1/mcp`; `make up` prints the port.

## Seeing and ending what you connected

**Settings, Connected assistants** lists each assistant you allowed, what it
may do and when it was last used. **Disconnect** stops it at once; connecting
it again asks you again.

Administrators see everybody's assistants under **Everyone's assistants**,
can disconnect any of them, and can switch assistants off for the whole
installation. Switching off refuses every assistant's token at once and
keeps the grants, so switching on again lets them back in.

The audit log names the assistant beside the person for everything done
through one.

## For operators

- `ROTOR_APP_URL` names the issuer and the MCP resource, so it must be the
  address people and assistants use, https in production.
- The web client's nginx serves `/.well-known/oauth-*` from the api; an
  ingress in front of it needs nothing more.
- Registering is limited to 10 an hour per caller's address. Behind a proxy,
  list the proxy's network in `ROTOR_TRUSTED_PROXIES` (chart:
  `api.trustedProxies`) so the address counted is the caller's.
