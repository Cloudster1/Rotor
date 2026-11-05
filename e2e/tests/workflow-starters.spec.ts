import { expect, test } from "@playwright/test";
import { expectAccessible } from "../fixtures/shell";

const STARTER = "story-to-requirements";

test("a person starts a new workflow from one Rotor ships, which saving checks as usual @desktop", async ({ page }) => {
  await page.goto("/workflows/new");
  const picker = page.locator("[data-starter-picker]");
  await expect(picker).toBeVisible();
  await expect(picker.locator('[data-starter="blank"]')).toBeChecked();
  await expectAccessible(page);

  await picker.locator(`[data-starter="${STARTER}"]`).check();
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
  await expect(page.locator(`[data-starter="${STARTER}"]`)).toBeChecked();
  await expect(page.locator("[data-yaml-editor] .cm-content")).toContainText(`name: ${STARTER}`);
});
