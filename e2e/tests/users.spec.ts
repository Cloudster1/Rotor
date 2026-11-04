import { expect, test } from "@playwright/test";
import { switchOn } from "../fixtures/access";
import { PEOPLE, signIn } from "../fixtures/auth";
import { expectAccessible, openShell, scrollsSideways } from "../fixtures/shell";

// Admin's Users page (#203). Only this spec signs dave in, so switching him off cannot trip another spec.
const DAVE_EMAIL = "dave@suite.test";

test("the Users page lists people with their groups and roles @desktop", async ({ page }) => {
  await openShell(page, "/admin/users");
  await expect(page.getByRole("heading", { level: 1, name: "Users" })).toBeVisible();
  await page.locator("[data-user-search]").fill("alice");
  const alice = page.locator('[data-user="alice@suite.test"]');
  await expect(alice).toBeVisible();
  await expect(alice).toContainText("engineering");
  await expect(alice).toContainText("Administrator");
  await expect(alice).toHaveAttribute("data-user-active", "true");
  await expect(page).toHaveURL(/\/admin\/users\?q=alice$/);
  await expectAccessible(page);
});

test("an administrator switches a person off in Users, which ends their session, and on again @desktop", async ({ page, browser }) => {
  const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  const dave = await context.newPage();
  try {
    await signIn(dave, PEOPLE.dave, "/workflows");

    await openShell(page, "/admin/users?q=dave");
    const row = page.locator(`[data-user="${DAVE_EMAIL}"]`);
    await expect(row).toHaveAttribute("data-user-active", "true");
    await row.locator('[data-action="switch-off"]').click();
    const dialog = page.locator("dialog[data-confirm]");
    await expect(dialog).toContainText("every session and connection of theirs ends");
    await expectAccessible(page);
    await dialog.locator('[data-action="confirm"]').click();
    await expect(dialog).toHaveCount(0);
    await expect(row).toHaveAttribute("data-user-active", "false");
    await expect(row).toContainText("Switched off");
    await expect(page.getByRole("status")).toContainText(`Switched off ${PEOPLE.dave.name}`);

    // His session ended with it, so the next page he asks for sends him to sign in.
    await dave.reload();
    await expect(dave).toHaveURL(/\/login/);

    await row.locator('[data-action="switch-on"]').click();
    await expect(row).toHaveAttribute("data-user-active", "true");
    await expect(page.getByRole("status")).toContainText(`Switched on ${PEOPLE.dave.name}`);
    // A browser of his own again, so the sign-in goes through the provider's page and not a session it kept.
    const again = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    try {
      const back = await again.newPage();
      await signIn(back, PEOPLE.dave, "/workflows");
      await expect(back.locator("main h1")).toHaveText("Workflows");
    } finally {
      await again.close();
    }
  } finally {
    await context.close();
    await switchOn(page, DAVE_EMAIL);
  }
});

test("the server's refusal to switch off your own account is said in the dialog @desktop", async ({ page }) => {
  await openShell(page, "/admin/users?q=alice");
  const row = page.locator('[data-user="alice@suite.test"]');
  await row.locator('[data-action="switch-off"]').click();
  const dialog = page.locator("dialog[data-confirm]");
  await dialog.locator('[data-action="confirm"]').click();
  await expect(dialog.getByRole("alert")).toBeVisible();
  await expect(row).toHaveAttribute("data-user-active", "true");
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toHaveCount(0);
});

test("on a phone the Users page fits the screen @mobile", async ({ page }) => {
  await openShell(page, "/admin/users");
  await expect(page.locator("[data-users]")).toBeVisible();
  expect(await scrollsSideways(page)).toBe(false);
  await expectAccessible(page);
});
