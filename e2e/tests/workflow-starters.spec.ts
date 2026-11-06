import { expect, test } from "@playwright/test";
import { expectAccessible } from "../fixtures/shell";

const STARTER = "story-to-requirements";

test("a person starts a new workflow from one Rotor ships, which saving checks as usual @desktop", async ({ page }) => {
  await page.goto("/workflows/new");
  const picker = page.locator("[data-starter-picker]");
  await expect(picker).toContainText("Start from: Blank workflow");
  await picker.locator("summary").click();
  await expect(picker.locator('[data-starter="blank"]')).toBeChecked();
  await expectAccessible(page);

  await picker.locator(`[data-starter="${STARTER}"]`).check();
  // The open list outgrows the editor, so checking a starter low in it scrolls the editor's first lines out of view,
  // and CodeMirror draws only the lines in view.
  await page.locator("[data-yaml-editor]").evaluate((editor) => editor.scrollIntoView());
  await expect(page.locator("[data-yaml-editor] .cm-content")).toContainText(`name: ${STARTER}`);
  // The stack connects no Armature, so saving checks the starter as usual and names the tool an administrator must add.
  const checks = page.locator("[data-problems]");
  await expect(checks).toContainText("Rotor has no tool called armature");
  await page.locator('[data-action="save-workflow"]').click();
  await expect(page.locator("[data-save-error]")).toContainText("Rotor has no tool called armature");
  await expect(page).toHaveURL(/\/workflows\/new$/);
});

test("an address names the workflow to start from @desktop", async ({ page }) => {
  await page.goto(`/workflows/new?start=${STARTER}`);
  await expect(page.locator("[data-starter-picker] summary")).toContainText(`Start from: ${STARTER}`);
  await expect(page.locator(`[data-starter="${STARTER}"]`)).toBeChecked();
  await expect(page.locator("[data-yaml-editor] .cm-content")).toContainText(`name: ${STARTER}`);
});
