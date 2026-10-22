// Prints Exciter's or Armature's tools/list as its administrator sees it, for the tool-stub; mk/tools.mk runs it
// with the address and the TOOL_* settings. The personal token it makes is ended again.
const base = process.argv[2] ?? process.env.TOOL_BASE_URL;
if (!base) {
  console.error("Name the tool's address: node tool-snapshot.mjs http://localhost:<port>, or set TOOL_BASE_URL.");
  process.exit(2);
}
const email = process.env.TOOL_ADMIN_EMAIL ?? process.env.EXCITER_ADMIN_EMAIL ?? "admin@exciter.test";
const password = process.env.TOOL_ADMIN_PASSWORD ?? process.env.EXCITER_ADMIN_PASSWORD ?? "exciter admin password";
// Set, it first makes that person the owner of a fresh installation, as Armature's first sign-up does.
const signupName = process.env.TOOL_SIGNUP_NAME;
const tokenLife = 10 * 60 * 1000;
const secondFactor = 202;

async function call(method, path, { cookie, bearer, body } = {}) {
  const headers = { "Content-Type": "application/json" };
  if (cookie) headers.Cookie = cookie;
  if (bearer) headers.Authorization = `Bearer ${bearer}`;
  const res = await fetch(base + path, { method, headers, body: body && JSON.stringify(body) });
  if (!res.ok) throw new Error(`${method} ${path} answered ${res.status}: ${await res.text()}`);
  return res;
}

if (signupName) {
  await call("POST", "/api/v1/auth/signup", { body: { email, name: signupName, password } });
}
const login = await call("POST", "/api/v1/auth/login", { body: { email, password } });
if (login.status === secondFactor) {
  throw new Error(`${email} signs in with a second factor; record as an administrator without one.`);
}
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
