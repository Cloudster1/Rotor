import AxeBuilder from "@axe-core/playwright";
import { expect, type Page } from "@playwright/test";

/** The palettes the theme button steps through, as web/src/lib/theme.ts stores them. */
export type ColourScheme = "light" | "dark";
const THEME_STORAGE_KEY = "rotor.theme";

/**
 * The places of web/src/features/shell/places.ts, in the sidebar's order, as alice, an administrator, sees them;
 * a place that opens one of its pages names where it lands and that page's heading.
 */
export const PLACES = [
  { path: "/workflows", label: "Workflows", opens: "/workflows", heading: "Workflows" },
  { path: "/runs", label: "Runs", opens: "/runs", heading: "Runs" },
  { path: "/approvals", label: "Approvals", opens: "/approvals", heading: "Approvals" },
  { path: "/connections", label: "Connections", opens: "/connections", heading: "Connections" },
  { path: "/settings", label: "Settings", opens: "/settings/account", heading: "Account" },
  { path: "/admin", label: "Admin", opens: "/admin/access", heading: "Access" },
] as const;

/** The pages of Settings and Admin, as web/src/features/settings/groups.ts lists them. */
export const SETTINGS_PAGES = [
  { slug: "account", path: "/settings/account", label: "Account" },
  { slug: "assistants", path: "/settings/assistants", label: "Assistants" },
  { slug: "usage", path: "/settings/usage", label: "Usage" },
] as const;
export const ADMIN_PAGES = [
  { slug: "access", path: "/admin/access", label: "Access" },
  { slug: "users", path: "/admin/users", label: "Users" },
  { slug: "workspaces", path: "/admin/workspaces", label: "Workspaces" },
  { slug: "unattended-writes", path: "/admin/unattended-writes", label: "Unattended writes" },
  { slug: "providers", path: "/admin/providers", label: "Model providers" },
  { slug: "langsmith", path: "/admin/langsmith", label: "LangSmith" },
  { slug: "tools", path: "/admin/tools", label: "Tools" },
  { slug: "assistants", path: "/admin/assistants", label: "Connected apps" },
  { slug: "audit", path: "/admin/audit", label: "Audit log" },
  { slug: "usage", path: "/admin/usage", label: "Usage" },
  { slug: "retention", path: "/admin/retention", label: "Retention" },
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
