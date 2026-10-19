import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { createWorkflow } from "../fixtures/runs";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** A workflow without tools, which a trigger may start: it writes nothing anywhere. */
function report(name: string): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    `metadata: {name: ${name}}`,
    "inputs:",
    "  type: object",
    "  properties:",
    "    project: {type: string, title: Project}",
    "limits: {maxSteps: 5, timeout: 2m}",
    "entry: sum",
    "nodes:",
    "  - id: sum",
    "    type: code",
    "    output: {schema: {type: object}}",
    "    source: |",
    "      def run(state, ctx):",
    '          return {"project": state["inputs"].get("project", "")}',
    "",
  ].join("\n");
}

for (const scheme of SCHEMES) {
  test(`a person schedules a workflow as themselves, sees its next runs and turns it off, in ${scheme} @desktop`, async ({ page }) => {
    const name = `e2e-${randomUUID().slice(0, 12)}`;
    const workflowID = await createWorkflow(page, report(name));
    await startInScheme(page, scheme);
    await page.goto(`/workflows/${workflowID}`);
    const section = page.locator("[data-triggers]");
    await expect(section).toContainText("No triggers yet.");

    await section.locator('[data-action="add-trigger"]').click();
    const form = section.locator("[data-trigger-form]");
    await form.getByLabel("Name").fill("Mondays");
    await form.getByLabel("Cron fields").fill("* * * * *");
    await form.locator('[data-action="preview-schedule"]').click();
    await expect(form).toContainText("every 5 minutes at most");
    await form.getByLabel("Cron fields").fill("0 8 * * 1");
    await form.getByLabel("Time zone").selectOption("Europe/Berlin");
    await form.getByLabel("Project").fill("RTR");
    await form.locator('[data-action="preview-schedule"]').click();
    await expect(form.locator("[data-next-runs] li")).toHaveCount(5);
    await expectAccessible(page);
    await form.locator('[data-action="save-trigger"]').click();

    const row = section.locator('[data-trigger="Mondays"]');
    await expect(row.locator("[data-next-run]")).toContainText("Next run");
    await expect(row).toContainText("0 8 * * 1 in Europe/Berlin");
    await expectAccessible(page);

    // The box follows the server, so it changes once the trigger is saved off or on.
    const enabled = row.locator("[data-trigger-enabled]");
    await enabled.click();
    await expect(enabled).not.toBeChecked();
    await expect(row.locator("[data-next-run]")).toContainText("Not scheduled");
    await enabled.click();
    await expect(enabled).toBeChecked();
    await expect(row.locator("[data-next-run]")).toContainText("Next run");

    await row.locator('[data-action="remove-trigger"]').click();
    await row.locator('[data-action="confirm-remove-trigger"]').click();
    await expect(section).toContainText("No triggers yet.");
  });
}
