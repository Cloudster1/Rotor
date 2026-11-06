import { expect, request, test } from "@playwright/test";
import { randomBytes } from "node:crypto";
import { keycloakURL, WEB_URL } from "../fixtures/stack";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";

// A sender's own traces (#257): spans an app sends over OTLP that join no run, listed and opened in the web app.

// dynamo-connect is the realm's service account whose tokens name Rotor (#168), with its development secret, as
// SECURITY.md lists it; any account Rotor lets in may send spans (#243).
const SENDER_CLIENT = "dynamo-connect";
const SENDER_SECRET = "dynamo-connect-dev-secret";
const NS_PER_MS = 1_000_000n;
// OTLP's numbers for a span's kind and status: internal, and error.
const KIND_INTERNAL = 1;
const STATUS_ERROR = 2;
const FAILURE = "The lookup found no customer";
const SCHEMES: ColourScheme[] = ["light", "dark"];

/** Sends a root span that works and a child that fails, as an app's exporter would, and answers the trace id. */
async function sendTrace(service: string, root: string): Promise<string> {
  const anyone = await request.newContext();
  try {
    const token = await anyone.post(`${keycloakURL()}/realms/dynamo/protocol/openid-connect/token`, {
      form: { grant_type: "client_credentials", client_id: SENDER_CLIENT, client_secret: SENDER_SECRET },
    });
    expect(token.ok(), "the service account's token").toBe(true);
    const bearer = ((await token.json()) as { access_token: string }).access_token;
    const traceId = randomBytes(16).toString("hex");
    const rootId = randomBytes(8).toString("hex");
    const start = BigInt(Date.now()) * NS_PER_MS;
    const at = (ms: number) => String(start + BigInt(ms) * NS_PER_MS);
    const spans = [
      { traceId, spanId: rootId, name: root, kind: KIND_INTERNAL, startTimeUnixNano: at(0), endTimeUnixNano: at(40) },
      {
        traceId,
        spanId: randomBytes(8).toString("hex"),
        parentSpanId: rootId,
        name: "lookup",
        kind: KIND_INTERNAL,
        startTimeUnixNano: at(5),
        endTimeUnixNano: at(30),
        status: { code: STATUS_ERROR, message: FAILURE },
      },
    ];
    const sent = await anyone.post(`${WEB_URL}/api/v1/otlp/v1/traces`, {
      headers: { Authorization: `Bearer ${bearer}`, "Content-Type": "application/json" },
      data: { resourceSpans: [{ resource: { attributes: [{ key: "service.name", value: { stringValue: service } }] }, scopeSpans: [{ spans }] }] },
    });
    expect(sent.status(), await sent.text()).toBe(200);
    return traceId;
  } finally {
    await anyone.dispose();
  }
}

for (const scheme of SCHEMES) {
  test(`an administrator opens a trace an app sent over OTLP from the Traces list, the failed span opened, in ${scheme} @desktop`, async ({ page }) => {
    await startInScheme(page, scheme);
    const name = `e2e-own-${randomBytes(4).toString("hex")}`;
    const traceId = await sendTrace(`${name}-app`, name);

    await page.goto("/traces");
    const row = page.locator(`[data-trace="${traceId}"]`);
    await expect(row).toContainText(`${name}-app`);
    await expect(row.locator("[data-trace-failed]")).toHaveText("Failed");
    // alice administers Rotor, so she reads every sender's traces, each saying who sent it.
    await expect(row.locator("[data-trace-sender]")).not.toBeEmpty();
    await expectAccessible(page);

    await row.getByRole("link", { name }).click();
    await expect(page.getByRole("heading", { level: 1, name: `Trace ${name}` })).toBeVisible();
    const tree = page.getByRole("tree", { name: "Spans" });
    const failed = tree.locator('[data-span-name="lookup"]');
    await expect(failed).toHaveAttribute("aria-selected", "true");
    await expect(failed).toContainText("Failed");
    const details = page.getByRole("region", { name: "Span lookup" });
    await expect(details.locator("[data-span-detail-status]")).toHaveText("Failed");
    await expect(details).toContainText(FAILURE);
    await expect(page.locator('[data-trace-state="kept"]')).toContainText("2 spans");

    // The tree walks by keyboard as a run's does: up to the root, Enter shows it.
    await failed.focus();
    await page.keyboard.press("ArrowLeft");
    await expect(tree.locator(`[data-span-name="${name}"]`)).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("region", { name: `Span ${name}` })).toBeVisible();
    await expectAccessible(page);
  });
}
