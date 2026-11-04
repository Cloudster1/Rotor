import { expect, test } from "@playwright/test";
import { switchOn } from "../fixtures/access";
import { addStranger, PEOPLE, removeStranger, signIn } from "../fixtures/auth";
import { expectAccessible, openShell, scrollsSideways, startInScheme } from "../fixtures/shell";

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

// Signing in is not being let in (#204): a stranger of the realm waits, told so on the sign-in page, until an
// administrator lets them in from Users.
test("a person the realm knows waits until an administrator lets them in from Users @desktop", async ({ page, browser }) => {
  const erin = await addStranger(page.request, "erin");
  const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  try {
    const waiting = await context.newPage();
    await signIn(waiting, erin, "/workflows");
    await expect(waiting).toHaveURL(/\/login\?error=waiting/);
    await expect(waiting.locator("[data-sign-in-waiting]")).toContainText("nobody has let you into Rotor yet");
    await expectAccessible(waiting);

    await openShell(page, "/admin/users");
    const request = page.locator(`[data-join-request="${erin.email}"]`);
    await expect(page.getByRole("heading", { level: 2, name: "Waiting to be let in" })).toBeVisible();
    await expect(request).toContainText(erin.name);
    await expect(request).toContainText(`${erin.email}, asked`);
    await expectAccessible(page);
    await request.locator('[data-action="admit-request"]').click();
    await expect(page.locator("[data-join-requests-said]")).toHaveText(`${erin.name} may sign in now`);
    await expect(request).toHaveCount(0);
    await page.locator("[data-user-search]").fill(erin.username);
    await expect(page.locator(`[data-user="${erin.email}"]`)).toHaveAttribute("data-user-active", "true");

    const again = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    try {
      const back = await again.newPage();
      await signIn(back, erin, "/workflows");
      await expect(back.locator("main h1")).toHaveText("Workflows");
    } finally {
      await again.close();
    }
  } finally {
    await context.close();
    await removeStranger(page.request, erin);
  }
});

// The requests above the table pass axe in both palettes, on a desktop and on a phone; the person is turned away after.
test("the people waiting on Users pass axe in light and dark", async ({ page, browser }) => {
  const finn = await addStranger(page.request, "finn");
  const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  try {
    await signIn(await context.newPage(), finn);
    for (const scheme of ["light", "dark"] as const) {
      const shown = await page.context().newPage();
      await startInScheme(shown, scheme);
      await openShell(shown, "/admin/users");
      await expect(shown.locator(`[data-join-request="${finn.email}"]`)).toBeVisible();
      await expect(shown.locator("html")).toHaveAttribute("data-theme", scheme);
      expect(await scrollsSideways(shown), "Users scrolls sideways").toBe(false);
      await expectAccessible(shown);
      await shown.close();
    }
    const requests = (await (await page.request.get("/api/v1/users/requests")).json()) as { requests: { userId: string; email: string }[] };
    const mine = requests.requests.find((each) => each.email === finn.email);
    expect(mine, "finn waits").toBeDefined();
    expect((await page.request.delete(`/api/v1/users/requests/${mine!.userId}`)).status()).toBe(204);
  } finally {
    await context.close();
    await removeStranger(page.request, finn);
  }
});
