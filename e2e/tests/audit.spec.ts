import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { readFile } from "node:fs/promises";
import { PEOPLE, signIn } from "../fixtures/auth";
import { createWorkflow } from "../fixtures/runs";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";

// Adapted from Exciter's audit spec (3f6c194): the log is filtered, then exported as it is filtered.

const SCHEMES: ColourScheme[] = ["light", "dark"];

function workflow(name: string): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    `metadata: {name: ${name}}`,
    "limits: {maxSteps: 3, timeout: 1m}",
    "entry: ask",
    "nodes:",
    "  - {id: ask, type: approval, message: 'Go on?'}",
    "",
  ].join("\n");
}

for (const scheme of SCHEMES) {
  test(`an administrator filters the audit log to one workflow and exports it, in ${scheme} @desktop`, async ({ page }) => {
    const workflowID = await createWorkflow(page, workflow(`e2e-${randomUUID().slice(0, 12)}`));
    await startInScheme(page, scheme);
    await page.goto("/settings");
    await page.locator("[data-audit-link] a").click();
    await expect(page.getByRole("heading", { level: 1, name: "Audit log" })).toBeVisible();

    await page.getByLabel("Action").selectOption("workflow.created");
    const row = page.locator('[data-audit-row="workflow.created"]').filter({ hasText: workflowID });
    await expect(row).toBeVisible();
    await row.locator('[data-action="filter-target"]').click();
    await expect(page.locator("[data-audit-target-filter]")).toBeVisible();
    await expect(page.locator("[data-audit-row]")).toHaveCount(1);
    await expectAccessible(page);

    const [csv] = await Promise.all([page.waitForEvent("download"), page.locator('[data-action="export-audit-csv"]').click()]);
    const csvText = await readFile((await csv.path()) as string, "utf8");
    const lines = csvText.trim().split("\n");
    expect(lines[0]).toBe("time,action,actor_id,actor,target_type,target_id,ip,request_id,data,via");
    expect(lines).toHaveLength(2);
    expect(lines[1]).toContain(workflowID);

    const [json] = await Promise.all([page.waitForEvent("download"), page.locator('[data-action="export-audit-json"]').click()]);
    const entries = JSON.parse(await readFile((await json.path()) as string, "utf8")) as { action: string; targetId: string }[];
    expect(entries).toEqual([expect.objectContaining({ action: "workflow.created", targetId: workflowID })]);

    // Each export is itself recorded; the page reads the log's actions afresh to offer it.
    await page.reload();
    await page.getByLabel("Action").selectOption("audit.exported");
    await expect(page.locator('[data-audit-row="audit.exported"]').first()).toBeVisible();
  });
}

test("a member is told the audit log is not theirs to read", async ({ browser }) => {
  const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  const page = await context.newPage();
  await signIn(page, PEOPLE.bob, "/settings/audit");
  await expect(page.getByRole("alert")).toHaveText("Only administrators read the audit log.");
  const response = await page.request.get("/api/v1/audit");
  expect(response.status()).toBe(403);
  await context.close();
});
