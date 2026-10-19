import { expect, test } from "@playwright/test";
import { expectAccessible, startInScheme } from "../fixtures/shell";

// The setting is one for the whole installation, so one test walks it, in both schemes, and puts it back.
test("an administrator changes how long finished runs are kept, within what Rotor allows @desktop", async ({ page }) => {
  await startInScheme(page, "light");
  await page.goto("/settings");
  const section = page.locator("[data-retention]");
  const days = section.locator("[data-retention-days]");
  const before = await section.locator("[data-retention-current]").getAttribute("data-retention-current");
  await expectAccessible(page);

  await days.fill("4000");
  await section.getByRole("button", { name: "Save" }).click();
  await expect(section.locator("[data-retention-error]")).toBeVisible();
  await expectAccessible(page);

  await days.fill("30");
  await section.getByRole("button", { name: "Save" }).click();
  await expect(section.locator("[data-retention-current]")).toHaveAttribute("data-retention-current", "30");

  await startInScheme(page, "dark");
  await page.goto("/settings");
  await expect(section.locator("[data-retention-current]")).toHaveAttribute("data-retention-current", "30");
  await expectAccessible(page);

  await days.fill(before ?? "90");
  await section.getByRole("button", { name: "Save" }).click();
  await expect(section.locator("[data-retention-current]")).toHaveAttribute("data-retention-current", before ?? "90");
});
