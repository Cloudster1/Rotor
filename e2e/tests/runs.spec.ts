import { expect, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { PEOPLE, signIn } from "../fixtures/auth";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { toolStubURL } from "../fixtures/stack";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** How long a run may take, sandbox start included, before the test gives up on it. */
const RUN_TIMEOUT_MS = 120_000;

/** A workflow that greets with its input in a code node, binding the tool when one is named. */
function greeter(name: string, tool?: string): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    `metadata: {name: ${name}}`,
    "inputs:",
    "  type: object",
    "  required: [greeting]",
    "  properties:",
    "    greeting: {type: string, title: Greeting, minLength: 1}",
    tool ? `tools:\n  t: {tool: ${tool}, scopes: [mcp], allowTools: [whoami]}` : "",
    "limits: {maxSteps: 5, timeout: 2m}",
    `entry: ${tool ? "ask" : "greet"}`,
    "nodes:",
    tool ? "  - {id: ask, type: tool, tool: t, call: whoami}" : "",
    "  - id: greet",
    "    type: code",
    "    output: {schema: {type: object, required: [text]}}",
    "    source: |",
    "      def run(state, ctx):",
    '          return {"text": state["inputs"]["greeting"] + " from the sandbox"}',
    tool ? "edges:\n  - {from: ask, to: greet}" : "",
    "",
  ]
    .filter((line) => line !== "")
    .join("\n");
}

/** Saves a workflow as whoever the page is signed in as, and answers its id. */
async function createWorkflow(page: Page, source: string): Promise<string> {
  const response = await page.request.post("/api/v1/workflows", { data: { specYAML: source, comment: "e2e" } });
  expect(response.status(), await response.text()).toBe(201);
  const body = (await response.json()) as { workflow: { id: string } };
  return body.workflow.id;
}

async function startFromDialog(page: Page, greeting: string): Promise<void> {
  const dialog = page.locator("[data-run-dialog]");
  await dialog.getByLabel("Greeting").fill(greeting);
  await dialog.locator('[data-action="start-run"]').click();
}

for (const scheme of SCHEMES) {
  test(`a person starts a run with its inputs and sees it end, in ${scheme}`, async ({ page }) => {
    const name = `e2e-${randomUUID().slice(0, 12)}`;
    await startInScheme(page, scheme);
    const id = await createWorkflow(page, greeter(name));
    await page.goto(`/workflows/${id}`);
    await page.locator('[data-action="run-workflow"]').click();
    const dialog = page.locator("[data-run-dialog]");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("heading")).toHaveText(`Run ${name}`);
    await expectAccessible(page);

    await startFromDialog(page, "Hello");
    await expect(page).toHaveURL(/\/runs\/[0-9a-f-]{36}$/);
    await expect(page.locator("[data-run-status]")).toHaveAttribute("data-run-status", "succeeded", { timeout: RUN_TIMEOUT_MS });
    await expect(page.locator("[data-run-output]")).toContainText("Hello from the sandbox");
    await expectAccessible(page);
  });
}

test("a run that acts in a tool the person has not connected offers it, then starts once connected @desktop", async ({ page, browser }) => {
  // A tenant named <product>--<rest> answers as that product, so this tool's name is the test's alone.
  const product = `e2e${randomUUID().slice(0, 8)}`;
  const address = `${toolStubURL()}/t/${product}--run`;
  await page.goto("/settings");
  const form = page.locator("[data-add-tool]");
  await form.getByLabel("Address").fill(address);
  await form.locator('[data-action="add-tool"]').click();
  await expect(page.locator("[data-tools] li", { hasText: address })).toHaveAttribute("data-status", "ready");
  const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  const bob = await context.newPage();
  try {
    // alice connects, so the workflow saves against what the tool offers, and shares it with everyone.
    await page.goto("/connections");
    await page.locator("li[data-connection]", { hasText: address }).locator('[data-action="connect"]').click();
    await page.waitForURL(`${toolStubURL()}/**`);
    await page.locator("#username").fill(PEOPLE.alice.username);
    await page.locator('button[data-action="allow"]').click();
    await expect(page).toHaveURL(/\/connections\?connected=/);
    const id = await createWorkflow(page, greeter(`e2e-${product}`, product));
    const shared = await page.request.put(`/api/v1/workflows/${id}/sharing`, { data: { sharing: "everyone", groups: [] } });
    expect(shared.status()).toBe(200);

    await signIn(bob, PEOPLE.bob, `/workflows/${id}`);
    await bob.locator('[data-action="run-workflow"]').click();
    await startFromDialog(bob, "Hi");
    const notice = bob.locator("[data-connect-first]");
    await expect(notice).toBeVisible();
    await expectAccessible(bob);
    await notice.locator(`[data-connect="${product}"]`).click();
    await bob.waitForURL(`${toolStubURL()}/**`);
    await bob.locator("#username").fill(PEOPLE.bob.username);
    await bob.locator('button[data-action="allow"]').click();
    await expect(bob).toHaveURL(new RegExp(`/workflows/${id}\\?connected=`));

    await bob.locator('[data-action="run-workflow"]').click();
    await startFromDialog(bob, "Hi");
    await expect(bob).toHaveURL(/\/runs\/[0-9a-f-]{36}$/);
    await expect(bob.locator("[data-run-status]")).toHaveAttribute("data-run-status", "succeeded", { timeout: RUN_TIMEOUT_MS });
    await expect(bob.locator("[data-run-output]")).toContainText("Hi from the sandbox");
  } finally {
    await context.close();
    await page.goto("/settings");
    const row = page.locator("[data-tools] li", { hasText: address });
    await row.locator('[data-action="remove-tool"]').click();
    await row.locator('[data-action="confirm-remove"]').click();
    await expect(row).toHaveCount(0);
  }
});
