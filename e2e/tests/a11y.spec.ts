import { expect, test } from "@playwright/test";
import { ADMIN_PAGES, expectAccessible, openShell, PLACES, SETTINGS_PAGES, startInScheme, type ColourScheme } from "../fixtures/shell";

// Contrast needs real layout and real colours, which is why these run here and not in the jsdom suite.
// The workflow editor is no sidebar place, but its canvas and inspector are pages of their own to check (#46),
// and so is every page of Settings and Admin (#199).
const PATHS = [
  ...new Set([...PLACES.map((place) => place.opens), ...[...SETTINGS_PAGES, ...ADMIN_PAGES].map((page) => page.path), "/workflows/new", "/no-such-page"]),
];
const SCHEMES: ColourScheme[] = ["light", "dark"];

for (const scheme of SCHEMES) {
  for (const path of PATHS) {
    test(`${path} passes axe in ${scheme}`, async ({ page }) => {
      await startInScheme(page, scheme);
      await openShell(page, path);
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expect(page.locator("main h1")).toBeVisible();
      await expectAccessible(page);
    });
  }
}
