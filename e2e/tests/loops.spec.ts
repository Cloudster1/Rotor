import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { createWorkflow, RUN_TIMEOUT_MS, startRun } from "../fixtures/runs";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** A code node a router sends round again until its three visits are used up, then on to done (#134). */
function looping(name: string): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    `metadata: {name: ${name}}`,
    "limits: {maxSteps: 20, timeout: 3m}",
    "entry: work",
    "nodes:",
    "  - id: work",
    "    type: code",
    "    maxVisits: 3",
    "    onExhausted: done",
    "    output: {schema: {type: object}}",
    "    source: |",
    "      def run(state, ctx):",
    '          return {"before": state["visits"].get("work", 0)}',
    "  - id: again",
    "    type: router",
    "    on: visits.work",
    "    cases: {'9': done}",
    "    default: work",
    "  - id: done",
    "    type: code",
    "    output: {schema: {type: object}}",
    "    source: |",
    "      def run(state, ctx):",
    '          return {"attempts": state["visits"]["work"]}',
    "edges:",
    "  - {from: work, to: again}",
    "",
  ].join("\n");
}

for (const scheme of SCHEMES) {
  test(`a loop is drawn as one, and its run numbers each visit until onExhausted takes over, in ${scheme} @desktop`, async ({ page }) => {
    const name = `e2e-${randomUUID().slice(0, 12)}`;
    await startInScheme(page, scheme);
    const workflow = await createWorkflow(page, looping(name));

    await page.goto(`/workflows/${workflow}`);
    await page.getByRole("tab", { name: "Canvas" }).click();
    const canvas = page.locator("[data-canvas]");
    await expect(canvas.locator('[data-node="again"]')).toBeVisible();
    await expect(canvas.locator('[data-edge-loop][data-from="again"][data-to="work"]')).toHaveCount(1);
    await expect(canvas.locator("[data-edge-loop]")).toHaveCount(1);
    await expectAccessible(page);

    const runID = await startRun(page, workflow);
    await page.goto(`/runs/${runID}`);
    await expect(page.locator("[data-run-status]")).toHaveAttribute("data-run-status", "succeeded", { timeout: RUN_TIMEOUT_MS });
    await expect(page.locator('[data-run-graph] [data-edge-loop][data-from="again"][data-to="work"]')).toHaveCount(1);
    const timeline = page.locator("[data-run-timeline]");
    await expect(timeline.locator('[data-entry="started"] [data-visit]')).toHaveCount(6);
    await expect(timeline.locator('[data-visit="3"]').first()).toContainText("Started work (code), visit 3");
    await expect(timeline.getByText("Started done (code)", { exact: true })).toBeVisible();
    await expect(page.locator('[data-node="done"]')).toHaveAttribute("data-node-state", "done");
    await expectAccessible(page);
  });
}
