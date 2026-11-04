import { expect, test } from "@playwright/test";
import { ADMIN_PAGES, expectAccessible, openShell, SETTINGS_PAGES, scrollsSideways } from "../fixtures/shell";

test("the settings nav is walked with the keyboard, and marks the page open @desktop", async ({ page }) => {
  await openShell(page, "/settings");
  await expect(page).toHaveURL(/\/settings\/profile$/);
  const nav = page.getByRole("navigation", { name: "Settings pages" });
  await expect(nav.locator('[aria-current="page"]')).toHaveText("Profile");

  await nav.locator('[data-settings-page="profile"]').focus();
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

test("each of a person's own pages opens with a sentence on what it is for", async ({ page }) => {
  for (const each of SETTINGS_PAGES) {
    await openShell(page, each.path);
    const heading = page.getByRole("heading", { level: 1, name: each.label });
    await expect(heading).toBeVisible();
    const intro = page.locator("main [data-page-intro]");
    await expect(intro).toHaveCount(1);
    await expect(intro).toContainText(each.intro);
    expect(await heading.evaluate((h, p) => !!(h.compareDocumentPosition(p!) & Node.DOCUMENT_POSITION_FOLLOWING), await intro.elementHandle())).toBe(true);
  }
  await expect(page.getByRole("navigation", { name: "Places" }).getByRole("link", { name: "Connections" })).toHaveCount(0);
});

test("the old connections address sends a browser to Tool accounts, with what the tool said", async ({ page }) => {
  await openShell(page, "/connections?error=denied");
  await expect(page).toHaveURL(/\/settings\/accounts\?error=denied$/);
  await expect(page.getByRole("heading", { level: 1, name: "Tool accounts" })).toBeVisible();
  await expect(page.locator("[data-connect-error]")).toBeVisible();
});

test("the theme page and the top bar's button choose one colour scheme between them", async ({ page }) => {
  await openShell(page, "/settings/theme");
  const html = page.locator("html");
  await page.getByRole("radio", { name: "Dark" }).check();
  await expect(html).toHaveAttribute("data-theme", "dark");
  await expect(page.locator('[data-action="theme"]')).toHaveAttribute("data-theme-choice", "dark");
  await page.reload();
  await expect(page.getByRole("radio", { name: "Dark" })).toBeChecked();
  await page.locator('[data-action="theme"]').click();
  await expect(page.getByRole("radio", { name: "As the system" })).toBeChecked();
  await expect(html).not.toHaveAttribute("data-theme");
  await expectAccessible(page);
});

test("a time zone chosen on the profile writes the dates in it, and following the browser again undoes it @desktop", async ({ page }) => {
  await openShell(page, "/settings/profile");
  const zone = page.getByRole("combobox", { name: "Time zone" });
  const preview = page.locator("[data-time-zone-preview]");
  try {
    await zone.selectOption("Pacific/Kiritimati");
    await page.getByRole("button", { name: "Save time zone" }).click();
    await expect(page.getByRole("status")).toHaveText("Time zone saved.");
    await page.reload();
    await expect(zone).toHaveValue("Pacific/Kiritimati");
    const there = await page.evaluate(() => new Date().toLocaleString(undefined, { dateStyle: "medium", timeZone: "Pacific/Kiritimati" }));
    await expect(preview).toContainText(there);
    await expectAccessible(page);
  } finally {
    await zone.selectOption("");
    await page.getByRole("button", { name: "Save time zone" }).click();
    await expect(page.getByRole("status")).toHaveText("Time zone saved.");
  }
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
