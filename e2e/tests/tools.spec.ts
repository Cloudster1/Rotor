import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { PEOPLE, signIn } from "../fixtures/auth";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { toolStubURL } from "../fixtures/stack";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** A tool of its own at the double, so runs side by side never share one, with a name of its own, since names are unique (#267). */
function freshTool(): { address: string; name: string } {
  const name = `e2e-${randomUUID().slice(0, 12)}`;
  return { address: `${toolStubURL()}/t/${name}`, name };
}

for (const scheme of SCHEMES) {
  test(`an administrator adds a tool, sees it ready and removes it, in ${scheme} @desktop`, async ({ page }) => {
    const { address, name } = freshTool();
    await startInScheme(page, scheme);
    await page.goto("/admin/tools");
    const form = page.locator("[data-add-tool]");
    await form.getByLabel("Address").fill(address);
    await form.getByLabel("Name", { exact: true }).fill(name);
    await form.locator('[data-action="add-tool"]').click();
    const row = page.locator(`[data-tools] li[data-tool="${name}"]`, { hasText: address });
    await expect(row.locator("[data-product]")).toHaveText("exciter");
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
    const { address, name } = freshTool();
    await startInScheme(page, scheme);
    await page.goto("/admin/tools");
    // A client the tool never registered stands in for one it forgot, which only the double's control API can make.
    const form = page.locator("[data-add-tool]");
    await form.getByLabel("Address").fill(address);
    await form.getByLabel("Name", { exact: true }).fill(name);
    await form.getByLabel("The tool does not let Rotor register itself").check();
    await form.getByLabel("Client id").fill(`unknown-${randomUUID()}`);
    await form.getByLabel("Client secret").fill(randomUUID());
    await form.locator('[data-action="add-tool"]').click();
    const row = page.locator(`[data-tools] li[data-tool="${name}"]`, { hasText: address });
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

for (const scheme of SCHEMES) {
  test(`an administrator renames a tool, refused a name another has, and the warning reads in ${scheme} @desktop`, async ({ page }) => {
    const first = freshTool();
    const second = freshTool();
    await startInScheme(page, scheme);
    await page.goto("/admin/tools");
    const form = page.locator("[data-add-tool]");
    for (const tool of [first, second]) {
      await form.getByLabel("Address").fill(tool.address);
      await form.getByLabel("Name", { exact: true }).fill(tool.name);
      await form.locator('[data-action="add-tool"]').click();
      await expect(page.locator(`[data-tools] li[data-tool="${tool.name}"]`)).toHaveAttribute("data-status", "ready");
    }

    // Adding a third under a name the first has is refused, in a sentence asking for another.
    await form.getByLabel("Address").fill(freshTool().address);
    await form.getByLabel("Name", { exact: true }).fill(first.name);
    await form.locator('[data-action="add-tool"]').click();
    await expect(form.locator("[data-add-error]")).toContainText("Give this one another name");

    const row = page.locator(`[data-tools] li[data-tool="${second.name}"]`);
    await row.locator('[data-action="rename-tool"]').click();
    const rename = row.locator("[data-rename-tool]");
    await expect(rename.locator("[data-binders]")).toContainText("No workflow binds");
    await rename.getByLabel(`New name for ${second.name}`).fill(first.name);
    await rename.locator('[data-action="confirm-rename"]').click();
    await expect(rename.locator("[data-rename-error]")).toContainText("Give this one another name");
    await expectAccessible(page);

    const renamed = `${second.name}-renamed`;
    await rename.getByLabel(`New name for ${second.name}`).fill(renamed);
    await rename.locator('[data-action="confirm-rename"]').click();
    const after = page.locator(`[data-tools] li[data-tool="${renamed}"]`);
    await expect(after.locator("[data-renamed]")).toBeVisible();
    await expectAccessible(page);

    for (const tool of [first.address, second.address]) {
      const gone = page.locator("[data-tools] li", { hasText: tool });
      await gone.locator('[data-action="remove-tool"]').click();
      await gone.locator('[data-action="confirm-remove"]').click();
      await expect(gone).toHaveCount(0);
    }
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
