import { expect, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { createWorkflow, RUN_TIMEOUT_MS, startRun, waitsForRuns } from "../fixtures/runs";
import { expectAccessible } from "../fixtures/shell";

// Search spans (#261): a failed span found across runs and opened in its run's trace, chosen there.

/** How long after a run ends its last spans may take to be kept: the runner sends them in batches seconds apart. */
const SPANS_KEPT_MS = 30_000;
const FAILURE = "The story has no title";

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

/** Searches the workflow's spans by name, failed ones only when asked, until its spans are kept, and answers the hit's row. */
async function search(page: Page, workflow: string, name: string, failedOnly: boolean) {
  await page.goto("/traces/search");
  const form = page.getByRole("form", { name: "Search" });
  await form.getByLabel("Name holds").fill(name);
  await form.getByLabel("Workflow").selectOption({ label: workflow });
  await form.getByLabel("Failed only").setChecked(failedOnly);
  await form.getByRole("button", { name: "Search" }).click();
  const hit = page
    .getByRole("table", { name: "Spans found" })
    .locator("[data-span-hit]")
    .filter({ has: page.getByRole("link", { name, exact: true }) });
  // The address holds the search, so reading it again asks again while the runner's last spans are on their way.
  await expect(async () => {
    await page.reload();
    await expect(hit).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: SPANS_KEPT_MS });
  return hit;
}

test("a person searches for a failed span across runs and opens it chosen in its run's trace @desktop", async ({ page }) => {
  waitsForRuns(1);
  const name = `e2e-${randomUUID().slice(0, 12)}`;
  const runID = await startRun(page, await createWorkflow(page, failing(name)));
  await expect
    .poll(async () => ((await (await page.request.get(`/api/v1/runs/${runID}`)).json()) as { status: string }).status, { timeout: RUN_TIMEOUT_MS })
    .toBe("failed");

  await page.goto("/runs");
  await page.locator("[data-span-search-open]").click();
  await expect(page.getByRole("heading", { level: 1, name: "Search spans" })).toBeVisible();
  const row = await search(page, name, "node file", true);
  await expect(row.locator("[data-span-hit-failed]")).toHaveText("Failed");
  await expect(row).toContainText(FAILURE);
  await expect(row.locator("[data-span-hit-where]")).toHaveText(`A run of ${name}`);
  await expect(page.locator("[data-span-search-failed]")).toBeChecked();
  await expectAccessible(page);

  await row.getByRole("link", { name: "node file" }).click();
  await expect(page).toHaveURL(new RegExp(`/runs/${runID}\\?span=[0-9a-f]{16}$`));
  const section = page.getByRole("region", { name: "Trace", exact: true });
  await expect(section.locator('[data-span-name="node file"]')).toHaveAttribute("aria-selected", "true");
  await expect(section.getByRole("region", { name: "Span node file" })).toContainText(FAILURE);
  await expectAccessible(page);

  // A span that did not fail is chosen too, though the run's page otherwise opens the one it failed in.
  const draft = await search(page, name, "node draft", false);
  await draft.getByRole("link", { name: "node draft" }).click();
  await expect(section.locator('[data-span-name="node draft"]')).toHaveAttribute("aria-selected", "true");
  await expect(section.locator('[data-span-name="node file"]')).toHaveAttribute("aria-selected", "false");
  await expectAccessible(page);
});
