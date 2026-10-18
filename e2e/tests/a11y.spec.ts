import { expect, test } from "@playwright/test";
import { expectAccessible, openShell, PLACES, startInScheme, type ColourScheme } from "../fixtures/shell";

// Contrast needs real layout and real colours, which is why these run here and not in the jsdom suite.
const PATHS = [...PLACES.map((place) => place.path), "/no-such-page"];
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
