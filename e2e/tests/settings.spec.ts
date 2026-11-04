import { expect, test } from "@playwright/test";
import { ADMIN_PAGES, expectAccessible, openShell, SETTINGS_PAGES, scrollsSideways } from "../fixtures/shell";

test("the settings nav is walked with the keyboard, and marks the page open @desktop", async ({ page }) => {
  await openShell(page, "/settings");
  await expect(page).toHaveURL(/\/settings\/account$/);
  const nav = page.getByRole("navigation", { name: "Settings pages" });
  await expect(nav.locator('[aria-current="page"]')).toHaveText("Account");

  await nav.locator('[data-settings-page="account"]').focus();
  for (const next of SETTINGS_PAGES.slice(1)) {
    await page.keyboard.press("Tab");
    await expect(nav.locator(`[data-settings-page="${next.slug}"]`)).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(new RegExp(`${next.path}$`));
    await expect(page.getByRole("heading", { level: 1, name: next.label })).toBeVisible();
    await expect(nav.locator('[aria-current="page"]')).toHaveText(next.label);
  }
  await expectAccessible(page);
});

test("every admin page opens from a deep link, under its section's heading @desktop", async ({ page }) => {
  for (const each of ADMIN_PAGES) {
    await openShell(page, each.path);
    await expect(page.getByRole("heading", { level: 1, name: each.label })).toBeVisible();
    const nav = page.getByRole("navigation", { name: "Admin pages" });
    await expect(nav.locator('[aria-current="page"]')).toHaveAttribute("data-settings-page", each.slug);
  }
  const nav = page.getByRole("navigation", { name: "Admin pages" });
  for (const heading of ["People and access", "Work", "Integrations", "Records"]) await expect(nav.getByText(heading, { exact: true })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Places" }).locator('[aria-current="page"]')).toHaveText("Admin");
  await expectAccessible(page);
});

test("admin opens on its first page, Access under People and access @desktop", async ({ page }) => {
  await openShell(page, "/admin");
  await expect(page).toHaveURL(new RegExp(`${ADMIN_PAGES[0].path}$`));
  await expect(page.getByRole("heading", { level: 1, name: "Access" })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Admin pages" }).getByRole("list").first()).toHaveAccessibleName("People and access");
});

test("on a narrow screen the admin nav is a row that scrolls sideways, and the page does not @mobile", async ({ page }) => {
  await openShell(page, "/admin/workspaces");
  const nav = page.getByRole("navigation", { name: "Admin pages" });
  const row = nav.locator(":scope > div");
  expect(await row.evaluate((element) => element.scrollWidth > element.clientWidth)).toBe(true);
  expect(await scrollsSideways(page)).toBe(false);

  const last = ADMIN_PAGES[ADMIN_PAGES.length - 1]!;
  const link = nav.locator(`[data-settings-page="${last.slug}"]`);
  await link.click();
  await expect(page).toHaveURL(new RegExp(`${last.path}$`));
  await expect(page.getByRole("heading", { level: 1, name: last.label })).toBeVisible();
  await expect(link).toHaveAttribute("aria-current", "page");
  await expect(link).toBeInViewport();
  expect(await scrollsSideways(page)).toBe(false);
  await expectAccessible(page);
});
