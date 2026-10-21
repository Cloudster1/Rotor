import { createHash, randomBytes } from "node:crypto";
import { expect, request, test } from "@playwright/test";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { WEB_URL } from "../fixtures/stack";

// Adapted from Exciter at 3b340ca, e2e/tests/oauth.spec.ts (#47).

// Nothing listens there; the browser is stopped on its way back and answered by the test instead.
const CALLBACK = "http://127.0.0.1:9/callback";
const STATE = "the-state";
const NAME = "E2E assistant";

// One registration a run, as registering is counted by the caller's address.
let client = "";
// The verifier of the last request asked for, which an assistant keeps to trade its code.
let verifier = "";

test.beforeAll(async () => {
  const anyone = await request.newContext({ baseURL: WEB_URL });
  try {
    const made = await anyone.post("/api/v1/oauth/register", { data: { redirect_uris: [CALLBACK], client_name: NAME } });
    expect(made.status(), "registering the spec's assistant").toBe(201);
    client = ((await made.json()) as { client_id: string }).client_id;
  } finally {
    await anyone.dispose();
  }
});

/** Begins connecting as the assistant would, and answers the request the consent page is sent with. */
async function askToConnect(scope = "mcp"): Promise<string> {
  const anyone = await request.newContext({ baseURL: WEB_URL });
  try {
    const resource = ((await (await anyone.get("/.well-known/oauth-protected-resource")).json()) as { resource: string }).resource;
    verifier = randomBytes(32).toString("base64url");
    const query = new URLSearchParams({
      response_type: "code",
      client_id: client,
      redirect_uri: CALLBACK,
      state: STATE,
      code_challenge: createHash("sha256").update(verifier).digest("base64url"),
      code_challenge_method: "S256",
      resource,
      scope,
    });
    const sent = await anyone.get(`/api/v1/oauth/authorize?${query}`, { maxRedirects: 0 });
    expect(sent.status()).toBe(302);
    const pending = new URL(sent.headers()["location"]!).searchParams.get("request");
    expect(pending).toBeTruthy();
    return pending!;
  } finally {
    await anyone.dispose();
  }
}

/** Trades a code for an access token as the assistant, and answers how MCP answers it now. */
async function tradeAndCall(code: string): Promise<() => Promise<number>> {
  const anyone = await request.newContext({ baseURL: WEB_URL });
  const traded = await anyone.post("/api/v1/oauth/token", {
    form: { grant_type: "authorization_code", code, redirect_uri: CALLBACK, code_verifier: verifier, client_id: client },
  });
  expect(traded.status()).toBe(200);
  const token = ((await traded.json()) as { access_token: string }).access_token;
  return async () =>
    (
      await anyone.post("/api/v1/mcp", {
        headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
        data: { jsonrpc: "2.0", id: 1, method: "tools/list" },
      })
    ).status();
}

const SCHEMES: ColourScheme[] = ["light", "dark"];

test.describe("connecting an assistant", { tag: "@desktop" }, () => {
  // One assistant, whose grant for alice each test replaces, so the tests take turns.
  test.describe.configure({ mode: "serial" });
  for (const scheme of SCHEMES) {
    test(`a person allows an assistant to read, sees it in Settings and disconnects it, in ${scheme}`, async ({ page }) => {
      await startInScheme(page, scheme);
      const pending = await askToConnect();
      let back: URL | undefined;
      await page.route(`${CALLBACK}**`, async (route) => {
        back = new URL(route.request().url());
        await route.fulfill({ status: 200, contentType: "text/html", body: "<!doctype html><title>Back</title><p>Back at the assistant.</p>" });
      });
      await page.goto(`/oauth/consent?request=${pending}`);
      const consent = page.locator(`[data-consent="${client}"]`);
      await expect(consent).toContainText(NAME);
      await expect(page.locator("[data-consent-host]")).toContainText("127.0.0.1:9");
      await expectAccessible(page);

      await consent.locator("[data-consent-read-only]").check();
      await consent.locator('[data-action="allow-assistant"]').click();
      await page.waitForURL(`${CALLBACK}**`);
      expect(back?.searchParams.get("state")).toBe(STATE);
      expect(back?.searchParams.get("code")).toMatch(/^rotor_oac_/);
      const status = await tradeAndCall(back!.searchParams.get("code")!);
      expect(await status()).toBe(200);

      await page.goto("/settings");
      const mine = page.locator("[data-assistants]");
      const grant = mine.locator(`[data-grant="${NAME}"]`);
      await expect(grant).toContainText("Reads only");
      await expectAccessible(page);
      await grant.locator('[data-action="disconnect-assistant"]').click();
      await expect(grant).toHaveCount(0);
      expect(await status()).toBe(401);
    });
  }

  test("a person denies an assistant and it is told so", async ({ page }) => {
    const pending = await askToConnect("mcp:read");
    let back: URL | undefined;
    await page.route(`${CALLBACK}**`, async (route) => {
      back = new URL(route.request().url());
      await route.fulfill({ status: 200, contentType: "text/html", body: "<!doctype html><title>Back</title><p>Back.</p>" });
    });
    await page.goto(`/oauth/consent?request=${pending}`);
    await expect(page.locator("[data-consent-read-only]")).toBeChecked();
    await expect(page.locator("[data-consent-read-only]")).toBeDisabled();
    await page.locator('[data-action="deny-assistant"]').click();
    await page.waitForURL(`${CALLBACK}**`);
    expect(back?.searchParams.get("error")).toBe("access_denied");
    expect(back?.searchParams.get("state")).toBe(STATE);
  });

  test("an assistant Rotor does not know is refused in a sentence", async ({ page }) => {
    await page.goto("/oauth/consent?error=invalid_client");
    await expect(page.locator("[data-consent-refused]")).toContainText("does not know this assistant");
    await expectAccessible(page);
  });
});
