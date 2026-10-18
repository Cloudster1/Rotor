// Prints the suite's Exciter tools/list, as its bootstrap administrator sees it, for the tool-stub to serve.
// Usage: node tool-snapshot.mjs <Exciter's web address>; the personal token it makes is ended again.
const base = process.argv[2];
if (!base) {
  console.error("Name Exciter's address: node tool-snapshot.mjs http://localhost:<port>");
  process.exit(2);
}
const email = process.env.EXCITER_ADMIN_EMAIL ?? "admin@exciter.test";
const password = process.env.EXCITER_ADMIN_PASSWORD ?? "exciter admin password";
const tokenLife = 10 * 60 * 1000;

async function call(method, path, { cookie, bearer, body } = {}) {
  const headers = { "Content-Type": "application/json" };
  if (cookie) headers.Cookie = cookie;
  if (bearer) headers.Authorization = `Bearer ${bearer}`;
  const res = await fetch(base + path, { method, headers, body: body && JSON.stringify(body) });
  if (!res.ok) throw new Error(`${method} ${path} answered ${res.status}: ${await res.text()}`);
  return res;
}

const login = await call("POST", "/api/v1/auth/login", { body: { email, password } });
const cookie = login.headers
  .getSetCookie()
  .map((c) => c.split(";")[0])
  .join("; ");
const made = await (
  await call("POST", "/api/v1/tokens", {
    cookie,
    body: { name: "Rotor tools/list snapshot", expiresAt: new Date(Date.now() + tokenLife).toISOString() },
  })
).json();
try {
  const answer = await (
    await call("POST", "/api/v1/mcp", {
      bearer: made.token.secret,
      body: { jsonrpc: "2.0", id: 1, method: "tools/list" },
    })
  ).json();
  if (!answer.result?.tools?.length) throw new Error(`tools/list answered ${JSON.stringify(answer)}`);
  process.stdout.write(`${JSON.stringify(answer.result, null, 2)}\n`);
} finally {
  await call("DELETE", `/api/v1/tokens/${made.token.id}`, { cookie });
}
