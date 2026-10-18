import { expect, test } from "@playwright/test";
import { endProviderSessions, PEOPLE, signIn } from "../fixtures/auth";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";

// These start with nobody signed in.
test.use({ storageState: { cookies: [], origins: [] } });

// The backchannel logout reaches the api from Keycloak in the background, so the session ends a moment later.
const BACKCHANNEL_TIMEOUT_MS = 10_000;

test("a visitor is sent to sign in and lands where they were headed @desktop", async ({ page }) => {
  await page.goto("/runs");
  await expect(page).toHaveURL(/\/login\?next=%2Fruns$/);
  await page.locator('[data-action="sso"]').click();
  await page.locator("#username").fill(PEOPLE.bob.username);
  await page.locator("#password").fill(`${PEOPLE.bob.username} password`);
  await page.locator("#kc-login").click();
  await expect(page).toHaveURL(/\/runs$/);
  await expect(page.locator("[data-signed-in-as]")).toHaveText(PEOPLE.bob.name);
});

test("the settings say who is signed in and with which role @desktop", async ({ page }) => {
  await signIn(page, PEOPLE.bob, "/settings");
  await expect(page.locator("[data-account] [data-role]")).toHaveText(PEOPLE.bob.role);
});

test("signing out ends the session @desktop", async ({ page }) => {
  await signIn(page, PEOPLE.bob, "/workflows");
  await page.locator('[data-action="sign-out"]').click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goto("/approvals");
  await expect(page).toHaveURL(/\/login\?next=%2Fapprovals$/);
});

test("ending the session in Keycloak ends it in Rotor @desktop", async ({ page, request }) => {
  await signIn(page, PEOPLE.carol, "/workflows");
  expect(await endProviderSessions(request, PEOPLE.carol.username)).toBeGreaterThan(0);
  await expect(async () => {
    await page.goto("/runs");
    await expect(page).toHaveURL(/\/login/, { timeout: 1_000 });
  }).toPass({ timeout: BACKCHANNEL_TIMEOUT_MS });
});

for (const scheme of ["light", "dark"] as ColourScheme[]) {
  test(`the sign-in page passes axe in ${scheme}`, async ({ page }) => {
    await startInScheme(page, scheme);
    await page.goto("/login?error=expired");
    await expect(page.getByRole("alert")).toBeVisible();
    await expectAccessible(page);
  });
}
