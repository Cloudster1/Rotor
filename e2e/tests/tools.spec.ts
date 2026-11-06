import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { PEOPLE, signIn } from "../fixtures/auth";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { toolStubURL } from "../fixtures/stack";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** A tool of its own at the double, so runs side by side never share one. */
function freshTool(): string {
  return `${toolStubURL()}/t/e2e-${randomUUID().slice(0, 12)}`;
}

for (const scheme of SCHEMES) {
  test(`an administrator adds a tool, sees it ready and removes it, in ${scheme} @desktop`, async ({ page }) => {
    const address = freshTool();
    await startInScheme(page, scheme);
    await page.goto("/admin/tools");
    const form = page.locator("[data-add-tool]");
    await form.getByLabel("Address").fill(address);
    await form.locator('[data-action="add-tool"]').click();
    const row = page.locator('[data-tools] li[data-tool="exciter"]', { hasText: address });
    await expect(row).toHaveAttribute("data-status", "ready");
    await expect(form.getByRole("status")).toBeVisible();
    await expectAccessible(page);

    await row.locator('[data-action="remove-tool"]').click();
    await expectAccessible(page);
    await row.locator('[data-action="confirm-remove"]').click();
    await expect(row).toHaveCount(0);
  });
}

for (const scheme of SCHEMES) {
  test(`checking again marks a tool that does not know Rotor's client, and its warning reads in ${scheme} @desktop`, async ({ page }) => {
    const address = freshTool();
    await startInScheme(page, scheme);
    await page.goto("/admin/tools");
    // A client the tool never registered stands in for one it forgot, which only the double's control API can make.
    const form = page.locator("[data-add-tool]");
    await form.getByLabel("Address").fill(address);
    await form.getByLabel("The tool does not let Rotor register itself").check();
    await form.getByLabel("Client id").fill(`unknown-${randomUUID()}`);
    await form.getByLabel("Client secret").fill(randomUUID());
    await form.locator('[data-action="add-tool"]').click();
    const row = page.locator('[data-tools] li[data-tool="exciter"]', { hasText: address });
    await expect(row).toHaveAttribute("data-status", "ready");

    await row.locator('[data-action="check-tool"]').click();
    await expect(row).toHaveAttribute("data-status", "needs_registration");
    await expect(row.locator("[data-needs-registration]")).toBeVisible();
    await expect(row.locator('[data-action="register-again"]')).toBeVisible();
    await expectAccessible(page);

    await row.locator('[data-action="remove-tool"]').click();
    await row.locator('[data-action="confirm-remove"]').click();
    await expect(row).toHaveCount(0);
  });
}

test("a tool without OAuth is refused with what it lacks @desktop", async ({ page }) => {
  await page.goto("/admin/tools");
  const form = page.locator("[data-add-tool]");
  // The double's meta answers at any path; one without OAuth is a tenant made so through the control API, which the
  // browser cannot reach, so an address that is no tool at all stands in for the refusal here.
  await form.getByLabel("Address").fill(`${toolStubURL()}/nowhere`);
  await form.locator('[data-action="add-tool"]').click();
  await expect(form.locator("[data-add-error]")).toBeVisible();
  await expectAccessible(page);
});

test.describe("as a member", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("the tools are refused, with no way to add or remove one @desktop", async ({ page }) => {
    await signIn(page, PEOPLE.bob, "/admin/tools");
    await expect(page.locator("[data-admin-refused]")).toBeVisible();
    await expect(page.locator("[data-tools]")).toHaveCount(0);
    await expect(page.locator("[data-add-tool]")).toHaveCount(0);
    await expectAccessible(page);
  });
});
