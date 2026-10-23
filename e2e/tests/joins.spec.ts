import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { createWorkflow, RUN_TIMEOUT_MS, startRun } from "../fixtures/runs";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** A code node one step long and another two steps long, run at once from test; gather waits for both (#135). */
function parallel(name: string): string {
  const code = (id: string, extra = "") => [
    `  - id: ${id}`,
    "    type: code",
    ...(extra ? [extra] : []),
    "    output: {schema: {type: object}}",
    "    source: |",
    "      def run(state, ctx):",
    `          return {"seen": sorted(state["nodes"])}`,
  ];
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    `metadata: {name: ${name}}`,
    "limits: {maxSteps: 20, timeout: 3m}",
    "entry: test",
    "nodes:",
    ...code("test"),
    ...code("review-code"),
    ...code("fix"),
    ...code("review-security"),
    ...code("gather", "    join: all"),
    "edges:",
    "  - {from: test, to: review-code}",
    "  - {from: test, to: review-security}",
    "  - {from: review-code, to: fix}",
    "  - {from: fix, to: gather}",
    "  - {from: review-security, to: gather}",
    "",
  ].join("\n");
}

for (const scheme of SCHEMES) {
  test(`parallel branches stand side by side on the run page, and their join runs once after both, in ${scheme} @desktop`, async ({ page }) => {
    const name = `e2e-${randomUUID().slice(0, 12)}`;
    await startInScheme(page, scheme);
    const workflow = await createWorkflow(page, parallel(name));

    const runID = await startRun(page, workflow);
    await page.goto(`/runs/${runID}`);
    await expect(page.locator("[data-run-status]")).toHaveAttribute("data-run-status", "succeeded", { timeout: RUN_TIMEOUT_MS });
    const graph = page.locator("[data-run-graph]");
    const node = (id: string) => graph.locator(`[data-node="${id}"]`);
    await expect(node("review-code")).toHaveAttribute("data-layer", "1");
    await expect(node("review-security")).toHaveAttribute("data-layer", "1");
    await expect(node("gather")).toHaveAttribute("data-layer", "3");
    const code = await node("review-code").boundingBox();
    const security = await node("review-security").boundingBox();
    expect(code && security && Math.abs(code.y - security.y) < 1 && code.x !== security.x).toBe(true);

    const timeline = page.locator("[data-run-timeline]");
    await expect(timeline.getByText("Started gather (code)", { exact: true })).toHaveCount(1);
    await expect(node("gather")).toHaveAttribute("data-node-state", "done");
    await expectAccessible(page);
  });
}
