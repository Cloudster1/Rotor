import { expect, test } from "@playwright/test";
import { buildVersion } from "../fixtures/stack";
import { openShell, PLACES, scrollsSideways } from "../fixtures/shell";

test("the index opens the first place", async ({ page }) => {
  await openShell(page, "/");
  await expect(page).toHaveURL(/\/workflows$/);
  await expect(page.getByRole("heading", { level: 1, name: "Workflows" })).toBeVisible();
});

test("the sidebar leads to every place and marks the one open", async ({ page }) => {
  await openShell(page, "/workflows");
  const nav = page.getByRole("navigation", { name: "Places" });
  for (const place of PLACES) {
    await nav.getByRole("link", { name: place.label }).click();
    await expect(page).toHaveURL(new RegExp(`${place.path}$`));
    await expect(page.getByRole("heading", { level: 1, name: place.label })).toBeVisible();
    await expect(nav.locator('[aria-current="page"]')).toHaveText(place.label);
  }
});

test("a chosen colour scheme survives a reload", async ({ page }) => {
  await openShell(page, "/runs");
  const button = page.locator('[data-action="theme"]');
  await expect(page.locator("html")).not.toHaveAttribute("data-theme");
  await button.click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await button.click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.reload();
  await expect(page.locator("main#main")).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(button).toHaveAttribute("data-theme-choice", "dark");
});

test("the settings name the build the server runs, through nginx and the api", async ({ page }) => {
  await openShell(page, "/settings");
  const version = page.locator("[data-build-version]");
  await expect(version).toBeVisible();
  const expected = buildVersion();
  if (expected) await expect(version).toHaveText(expected);
});

test("the skip link moves focus to the content", async ({ page }) => {
  await openShell(page, "/approvals");
  await page.keyboard.press("Tab");
  const skip = page.locator("[data-skip-link]");
  await expect(skip).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.locator("main#main")).toBeFocused();
});

test("a narrow screen never scrolls sideways @mobile", async ({ page }) => {
  for (const place of PLACES) {
    await openShell(page, place.path);
    expect(await scrollsSideways(page), place.path).toBe(false);
  }
});
