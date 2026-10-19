import { expect, type Page, test } from "@playwright/test";
import { createHmac, randomUUID } from "node:crypto";
import { createWorkflow } from "../fixtures/runs";
import { PEOPLE } from "../fixtures/auth";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { toolStubURL } from "../fixtures/stack";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** A workflow an event starts: it writes nothing anywhere and returns the requirement its event names. */
function echo(name: string): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    `metadata: {name: ${name}}`,
    "inputs: {type: object, required: [event], properties: {event: {type: object}}}",
    "limits: {maxSteps: 5, timeout: 2m}",
    "entry: echo",
    "nodes:",
    "  - id: echo",
    "    type: code",
    "    output: {schema: {type: object}}",
    "    source: |",
    "      def run(state, ctx):",
    '          return {"key": state["inputs"]["event"]["payload"]["requirement"]["key"]}',
    "",
  ].join("\n");
}

/** Adds a tool of its own at the double as alice and answers its address and id. */
async function addTool(page: Page): Promise<{ address: string; id: string }> {
  const address = `${toolStubURL()}/t/e2e-${randomUUID().slice(0, 12)}`;
  await page.goto("/settings");
  const form = page.locator("[data-add-tool]");
  await form.getByLabel("Address").fill(address);
  await form.locator('[data-action="add-tool"]').click();
  await expect(page.locator("[data-tools] li", { hasText: address })).toHaveAttribute("data-status", "ready");
  const tools = (await (await page.request.get("/api/v1/tools")).json()) as { tools: { id: string; baseURL: string }[] };
  return { address, id: tools.tools.find((t) => t.baseURL === address)?.id ?? "" };
}

/** Delivers an Exciter event to Rotor's hook for the tool, signed as Exciter signs it. */
async function deliver(page: Page, toolID: string, secret: string, key: string): Promise<number> {
  const body = JSON.stringify({
    version: 1,
    id: randomUUID(),
    source: "exciter",
    topic: "requirement.created",
    occurredAt: new Date().toISOString(),
    payload: { requirement: { key, project: { key: "RTR" } } },
  });
  const ts = Math.floor(Date.now() / 1000).toString();
  const signature = `sha256=${createHmac("sha256", secret).update(`${ts}.${body}`).digest("hex")}`;
  const response = await page.request.post(`/api/v1/hooks/${toolID}`, {
    data: body,
    headers: {
      "Content-Type": "application/json",
      "X-Exciter-Timestamp": ts,
      "X-Exciter-Signature-Timestamped-256": signature,
      "X-Exciter-Webhook-Version": "1",
    },
  });
  return response.status();
}

for (const scheme of SCHEMES) {
  test(`an administrator lets a tool's events in and a person starts a workflow from them, in ${scheme} @desktop`, async ({ page }) => {
    const { address, id } = await addTool(page);
    try {
      await startInScheme(page, scheme);
      await page.goto("/settings");
      const row = page.locator("[data-tools] li", { hasText: address });
      const hook = row.locator("[data-hook]");
      await expect(hook.locator("[data-hook-url]")).toHaveValue(new RegExp(`/api/v1/hooks/${id}$`));
      const secret = `exciter_whs_${randomUUID().replaceAll("-", "")}`;
      await hook.getByLabel("Webhook secret").fill(secret);
      await hook.locator('[data-action="save-hook-secret"]').click();
      await expect(hook).toContainText("Rotor keeps the secret in OpenBao.");
      await expect(hook.locator("[data-hook-secret]")).toHaveAttribute("data-hook-secret", "set");
      await expectAccessible(page);

      // The trigger's owner reads each event's subject as themselves, so alice connects first.
      await page.goto("/connections");
      const card = page.locator("li[data-connection]", { hasText: address });
      await card.locator('[data-action="connect"]').click();
      await page.waitForURL(`${toolStubURL()}/**`);
      await page.locator("#username").fill(PEOPLE.alice.username);
      await page.locator('button[data-action="allow"]').click();
      await expect(card).toHaveAttribute("data-status", "connected");

      const workflowID = await createWorkflow(page, echo(`e2e-${randomUUID().slice(0, 12)}`));
      await page.goto(`/workflows/${workflowID}`);
      const section = page.locator("[data-triggers]");
      await section.locator('[data-action="add-trigger"]').click();
      const form = section.locator("[data-trigger-form]");
      await form.getByLabel("Name").fill("On new requirements");
      await form.locator('[data-trigger-kind="event"]').check();
      await form.locator("[data-event-tool]").selectOption(id);
      await form.locator('[data-event-topic="requirement.created"]').check();
      await form.locator("[data-event-projects]").fill("RTR");
      await expectAccessible(page);
      await form.locator('[data-action="save-trigger"]').click();
      const trigger = section.locator('[data-trigger="On new requirements"]');
      await expect(trigger.locator("[data-trigger-events]")).toContainText("requirement.created");

      // The double does not show alice requirement RTR-1, so the firing is skipped and no run starts.
      expect(await deliver(page, id, secret, "RTR-1")).toBe(202);
      await expect(async () => {
        await page.reload();
        await expect(section.locator('[data-trigger="On new requirements"] [data-last-fire]')).toHaveAttribute("data-last-fire", "skipped", { timeout: 1_000 });
      }).toPass({ timeout: 60_000 });
      await expectAccessible(page);
    } finally {
      await page.goto("/settings");
      const row = page.locator("[data-tools] li", { hasText: address });
      await row.locator('[data-action="remove-tool"]').click();
      await row.locator('[data-action="confirm-remove"]').click();
      await expect(row).toHaveCount(0);
    }
  });
}
