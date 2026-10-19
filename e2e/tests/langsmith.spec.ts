import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { expectAccessible, startInScheme } from "../fixtures/shell";

// The setting is one for the whole installation, so one test walks it, in both schemes, and leaves it off.
test("an administrator turns LangSmith on only after confirming that run data leaves, and off again @desktop", async ({ page }) => {
  await startInScheme(page, "light");
  await page.goto("/settings");
  const section = page.locator("[data-langsmith]");
  await expect(section.locator("[data-langsmith-state]")).toHaveAttribute("data-langsmith-state", "off");
  await expectAccessible(page);

  await section.locator("[data-langsmith-enabled]").check();
  await section.getByLabel("Key", { exact: true }).fill(`lsv2_pt_${randomUUID().replaceAll("-", "")}`);
  await section.locator('[data-action="save-langsmith"]').click();
  await expect(section.locator("[data-langsmith-error]")).toContainText("Confirm that this is allowed");
  await expectAccessible(page);

  await section.locator("[data-langsmith-acknowledge]").check();
  await section.locator('[data-action="save-langsmith"]').click();
  await expect(section.locator("[data-langsmith-state]")).toHaveAttribute("data-langsmith-state", "on");
  await expect(section.getByLabel("Key", { exact: true })).toHaveValue("");

  await startInScheme(page, "dark");
  await page.goto("/settings");
  await expect(section.locator("[data-langsmith-state]")).toHaveAttribute("data-langsmith-state", "on");
  await expectAccessible(page);

  await section.locator('[data-action="remove-langsmith-key"]').click();
  await section.locator('[data-action="confirm-remove-langsmith-key"]').click();
  await expect(section.locator("[data-langsmith-state]")).toHaveAttribute("data-langsmith-state", "off");
});
