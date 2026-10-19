import { expect, test } from "@playwright/test";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";

const SCHEMES: ColourScheme[] = ["light", "dark"];

// A budget no test reaches, so setting it holds no other test's runs back while the browsers run side by side.
const HARMLESS_BUDGET = "999999";

// Both copies set and clear the one installation-wide budget, so side by side one would read the other's.
test.describe.configure({ mode: "serial" });

for (const scheme of SCHEMES) {
  test(`an administrator sees the usage and sets the limits on everyone, in ${scheme} @desktop`, async ({ page }) => {
    await startInScheme(page, scheme);
    await page.goto("/settings");
    await page.locator("[data-usage-link]").click();
    await expect(page.getByRole("heading", { level: 1, name: "Usage" })).toBeVisible();
    await expect(page.getByRole("region", { name: "Today" })).toBeVisible();
    await expectAccessible(page);

    const limits = page.locator("[data-limits]");
    await limits.getByLabel("Budget a day, in USD").fill(HARMLESS_BUDGET);
    await limits.locator('[data-action="save-limits"]').click();
    await expect(limits.getByRole("status")).toHaveText("Limits saved.");
    await expect(page.locator("[data-your-limits]")).toContainText(`${HARMLESS_BUDGET}.00 USD`);
    await expectAccessible(page);

    await limits.getByLabel("Budget a day, in USD").fill("");
    await limits.locator('[data-action="save-limits"]').click();
    await expect(page.locator("[data-your-limits]")).not.toContainText(`${HARMLESS_BUDGET}.00 USD`);
  });
}
