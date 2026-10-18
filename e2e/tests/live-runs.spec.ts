import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { createWorkflow, RUN_TIMEOUT_MS, startRun } from "../fixtures/runs";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** Two code nodes, the first printing as it goes and pausing for seconds, so a page sees the run move. */
function stepwise(name: string, seconds: number): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    `metadata: {name: ${name}}`,
    "limits: {maxSteps: 5, timeout: 3m}",
    "entry: first",
    "nodes:",
    "  - id: first",
    "    type: code",
    "    timeout: 2m",
    "    output: {schema: {type: object}}",
    "    source: |",
    "      import time",
    "      def run(state, ctx):",
    `          for i in range(${seconds}):`,
    '              print("step", i)',
    "              time.sleep(1)",
    '          return {"steps": 3}',
    "  - id: second",
    "    type: code",
    "    output: {schema: {type: object}}",
    "    source: |",
    "      def run(state, ctx):",
    '          return {"done": True}',
    "edges:",
    "  - {from: first, to: second}",
    "",
  ].join("\n");
}

for (const scheme of SCHEMES) {
  test(`a person follows a run live, finds it among the runs, and cancels another, in ${scheme}`, async ({ page }) => {
    const name = `e2e-${randomUUID().slice(0, 12)}`;
    await startInScheme(page, scheme);
    const workflow = await createWorkflow(page, stepwise(name, 3));
    const runID = await startRun(page, workflow);

    await page.goto(`/runs/${runID}`);
    await expect(page.locator("[data-run-graph]")).toBeVisible();
    await expect(page.locator('[data-node="first"]')).toBeVisible();
    await expect(page.locator('[data-run-timeline] [data-entry="log"]').first()).toContainText("step", { timeout: RUN_TIMEOUT_MS });
    await expect(page.locator('[data-node="first"]')).toHaveAttribute("data-node-state", /running|done/);
    await expectAccessible(page);
    await expect(page.locator("[data-run-status]")).toHaveAttribute("data-run-status", "succeeded", { timeout: RUN_TIMEOUT_MS });
    await expect(page.locator('[data-node="second"]')).toHaveAttribute("data-node-state", "done");
    await expect(page.locator('[data-run-timeline] [data-entry="run"]')).toBeVisible();

    await page.goto(`/runs?mine=true&workflow=${workflow}`);
    const row = page.locator(`[data-run="${runID}"]`);
    await expect(row.locator("[data-run-status]")).toHaveAttribute("data-run-status", "succeeded");
    await expectAccessible(page);
    await row.getByRole("link").click();
    await expect(page).toHaveURL(new RegExp(`/runs/${runID}$`));

    const long = await createWorkflow(page, stepwise(`${name}-long`, 60));
    const longRun = await startRun(page, long);
    await page.goto(`/runs/${longRun}`);
    await expect(page.locator('[data-run-timeline] [data-entry="log"]').first()).toBeVisible({ timeout: RUN_TIMEOUT_MS });
    await page.locator('[data-action="cancel-run"]').click();
    await expectAccessible(page);
    await page.locator('[data-action="confirm-cancel-run"]').click();
    await expect(page.locator("[data-run-status]")).toHaveAttribute("data-run-status", "cancelled");
    await expect(page.locator('[data-run-timeline] [data-entry="run"]')).toBeVisible();
    await expect(page.locator('[data-action="cancel-run"]')).toHaveCount(0);
  });
}
