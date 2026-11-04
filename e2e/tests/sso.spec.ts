import { expect, type Page, test } from "@playwright/test";
import { expectAccessible, openShell, startInScheme } from "../fixtures/shell";

// The Single sign-on tab of Access (#204). Every spec here changes how the whole stack signs in, so they run one
// after another and each puts the provider back as it found it.
test.describe.configure({ mode: "serial" });

interface Held {
  provider: { issuer: string; clientId: string; groupsClaim: string; admitByGroup: boolean; hasSecret: boolean } | null;
}

async function held(page: Page): Promise<Held> {
  const answer = await page.request.get("/api/v1/oidc-provider");
  expect(answer.ok(), "reading single sign-on").toBe(true);
  return (await answer.json()) as Held;
}

/** Puts single sign-on back as it was, keeping the secret held. */
async function restore(page: Page, was: Held): Promise<void> {
  const { issuer, clientId, groupsClaim, admitByGroup } = was.provider!;
  const put = await page.request.put("/api/v1/oidc-provider", { data: { issuer, clientId, groupsClaim, admitByGroup, enabled: true } });
  expect(put.ok(), "putting single sign-on back").toBe(true);
}

test("the Single sign-on tab saves a change and says a secret is held without showing it @desktop", async ({ page }) => {
  const was = await held(page);
  expect(was.provider?.hasSecret, "the stack seeds a secret").toBe(true);
  try {
    await openShell(page, "/admin/access?tab=sso");
    const form = page.locator("[data-sso]");
    await expect(form.locator('[data-sso-field="issuer"]')).toHaveValue(was.provider!.issuer);
    const secret = form.locator('[data-sso-field="clientSecret"]');
    await expect(secret).toHaveValue("");
    await expect(secret).toHaveAttribute("type", "password");
    await expect(secret).toHaveAttribute("placeholder", "Stored. Leave blank to keep it.");
    await expect(form).toContainText("It cannot be turned off");
    await expect(page.locator('[data-action="turn-off-sso"]')).toHaveCount(0);

    await form.getByRole("checkbox", { name: "Let people in by their groups" }).check();
    await form.locator('[data-action="save-sso"]').click();
    await expect(form.locator("[data-sso-saved]")).toHaveText("Saved. It applies from the next sign-in.");
    await expect(form).toContainText("Last changed by Alice Admin.");

    // What the page was answered with holds whether a secret is kept, never the secret.
    const answer = await page.request.get("/api/v1/oidc-provider");
    const text = await answer.text();
    expect(text).not.toContain("rotor-dev-secret");
    expect(JSON.parse(text).provider).toMatchObject({ hasSecret: true, admitByGroup: true });
    await page.reload();
    await expect(page.locator("[data-admit-by-group]")).toBeChecked();
    await expect(page.locator('[data-sso-field="clientSecret"]')).toHaveValue("");
  } finally {
    await restore(page, was);
  }
});

test("a single sign-on that could not work is refused beside its field @desktop", async ({ page }) => {
  const was = await held(page);
  try {
    await openShell(page, "/admin/access?tab=sso");
    const issuer = page.locator('[data-sso-field="issuer"]');
    await issuer.fill("");
    await page.locator('[data-action="save-sso"]').click();
    await expect(page.getByRole("alert")).toContainText("needs an issuer and a client ID");
    await expect(issuer).toHaveAttribute("aria-invalid", "true");
    await expectAccessible(page);
    expect((await held(page)).provider?.issuer).toBe(was.provider!.issuer);
  } finally {
    await restore(page, was);
  }
});

// The tab passes axe in both palettes, on a desktop and on a phone.
for (const scheme of ["light", "dark"] as const) {
  test(`the Single sign-on tab passes axe in ${scheme}`, async ({ page }) => {
    await startInScheme(page, scheme);
    await openShell(page, "/admin/access?tab=sso");
    await expect(page.locator('[data-sso-field="issuer"]')).toBeVisible();
    await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
    await expectAccessible(page);
  });
}
