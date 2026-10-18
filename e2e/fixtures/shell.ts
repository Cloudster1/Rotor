import AxeBuilder from "@axe-core/playwright";
import { expect, type Page } from "@playwright/test";

/** The palettes the theme button steps through, as web/src/lib/theme.ts stores them. */
export type ColourScheme = "light" | "dark";
const THEME_STORAGE_KEY = "rotor.theme";

/** The places of web/src/features/shell/places.ts, in the sidebar's order. */
export const PLACES = [
  { path: "/workflows", label: "Workflows" },
  { path: "/runs", label: "Runs" },
  { path: "/approvals", label: "Approvals" },
  { path: "/connections", label: "Connections" },
  { path: "/settings", label: "Settings" },
] as const;

/** Makes every page this browser opens start in the given palette, the way a stored choice does. */
export async function startInScheme(page: Page, scheme: ColourScheme): Promise<void> {
  await page.addInitScript(
    ([key, value]) => {
      try {
        localStorage.setItem(key, value);
      } catch {
        // An opaque origin has no storage; the page it opens is not ours.
      }
    },
    [THEME_STORAGE_KEY, scheme] as const,
  );
}

/** Opens a path and waits until the shell has drawn the page. */
export async function openShell(page: Page, path: string): Promise<void> {
  await page.goto(path);
  await expect(page.locator("main#main")).toBeVisible();
}

// Both named so a change of axe's defaults cannot quietly drop the rules jsdom cannot judge.
const RULES_ON = { "color-contrast": { enabled: true }, region: { enabled: true } };

/** Runs axe over the whole page and fails with every violation it finds. */
export async function expectAccessible(page: Page): Promise<void> {
  const results = await new AxeBuilder({ page }).options({ rules: RULES_ON }).analyze();
  const found = results.violations.flatMap((violation) =>
    violation.nodes.map((node) => `${violation.id} (${violation.impact}) at ${node.target.join(" ")}: ${node.failureSummary ?? violation.help}`),
  );
  expect(found, "axe found accessibility violations").toEqual([]);
}

/** Whether the page scrolls sideways, which a narrow screen must never do. */
export async function scrollsSideways(page: Page): Promise<boolean> {
  return page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
}
