import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { createWorkflow, RUN_TIMEOUT_MS, startRun, waitsForRuns } from "../fixtures/runs";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";

// A run's trace on its page (#245): the runner's spans of a real run, kept by the api and read back through #244.

/** How long after a run ends its last spans may take to be kept: the runner sends them in batches seconds apart. */
const SPANS_KEPT_MS = 30_000;
const FAILURE = "The story has no title";
const SCHEMES: ColourScheme[] = ["light", "dark"];

/** A code node that works, then one that fails, so the run fails in a span of its own. */
function failing(name: string): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    `metadata: {name: ${name}}`,
    "limits: {maxSteps: 5, timeout: 3m}",
    "entry: draft",
    "nodes:",
    "  - id: draft",
    "    type: code",
    "    output: {schema: {type: object}}",
    "    source: |",
    "      def run(state, ctx):",
    '          return {"title": ""}',
    "  - id: file",
    "    type: code",
    "    output: {schema: {type: object}}",
    "    source: |",
    "      def run(state, ctx):",
    `          raise ValueError("${FAILURE}")`,
    "edges:",
    "  - {from: draft, to: file}",
    "",
  ].join("\n");
}

for (const scheme of SCHEMES) {
  test(`a person reads a failed run's trace on its page, the failed span opened, by keyboard too, in ${scheme} @desktop`, async ({ page }) => {
    waitsForRuns(1);
    await startInScheme(page, scheme);
    const workflow = await createWorkflow(page, failing(`e2e-${randomUUID().slice(0, 12)}`));
    const runID = await startRun(page, workflow);
    const setting = (await (await page.request.get("/api/v1/settings/traces")).json()) as { keepContent: boolean };

    await page.goto(`/runs/${runID}`);
    await expect(page.locator("[data-run-status]")).toHaveAttribute("data-run-status", "failed", { timeout: RUN_TIMEOUT_MS });
    const section = page.getByRole("region", { name: "Trace", exact: true });
    const tree = section.getByRole("tree", { name: "Trace" });
    const failedNode = tree.locator('[data-span-name="node file"]');
    await expect(failedNode).toHaveAttribute("aria-selected", "true", { timeout: SPANS_KEPT_MS });
    await expect(failedNode).toContainText("Failed");
    await expect(tree.locator('[data-span-name="node draft"]')).not.toHaveAttribute("data-span-failed");

    const details = section.getByRole("region", { name: "Span node file" });
    await expect(details.locator("[data-span-detail-status]")).toHaveText("Failed");
    await expect(details).toContainText(FAILURE);
    if (!setting.keepContent) await expect(section.locator("[data-trace-no-text]")).toBeVisible();
    await expect(section.locator('[data-trace-state="kept"]')).toContainText("days after its last span");
    await expectAccessible(page);

    // The keyboard walks the tree: Home goes to the run's own span, Enter shows it.
    await failedNode.focus();
    await page.keyboard.press("Home");
    const root = tree.getByRole("treeitem").first();
    await expect(root).toBeFocused();
    await expect(root).toHaveAttribute("data-span-name", "run");
    await page.keyboard.press("Enter");
    await expect(section.getByRole("region", { name: "Span run" })).toBeVisible();
    await page.keyboard.press("ArrowLeft");
    await expect(root).toHaveAttribute("aria-expanded", "false");
    await expect(failedNode).toHaveCount(0);
    await page.keyboard.press("ArrowRight");
    await expect(failedNode).toBeVisible();
    await expectAccessible(page);
  });
}
