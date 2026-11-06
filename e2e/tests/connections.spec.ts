import { expect, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { PEOPLE, signIn } from "../fixtures/auth";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { toolStubURL } from "../fixtures/stack";
import { test } from "../fixtures/tools";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** Adds a tool of its own at the double as alice, the administrator the default browser is signed in as. */
async function addTool(page: Page, trackTool: (address: string) => void): Promise<string> {
  const address = `${toolStubURL()}/t/e2e-${randomUUID().slice(0, 12)}`;
  trackTool(address);
  await page.goto("/admin/tools");
  const form = page.locator("[data-add-tool]");
  await form.getByLabel("Address").fill(address);
  // Names are unique (#267), so a tool of its own also takes a name of its own beside others' Exciters.
  await form.getByLabel("Name", { exact: true }).fill(address.split("/t/")[1] ?? "");
  await form.locator('[data-action="add-tool"]').click();
  await expect(page.locator("[data-tools] li", { hasText: address })).toHaveAttribute("data-status", "ready");
  return address;
}

for (const scheme of SCHEMES) {
  test(`a person connects at the tool's consent page, sees what was allowed and disconnects, in ${scheme} @desktop`, async ({ page, browser, trackTool }) => {
    const address = await addTool(page, trackTool);
    const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    const bob = await context.newPage();
    try {
      await startInScheme(bob, scheme);
      await signIn(bob, PEOPLE.bob, "/settings/accounts");
      const card = bob.locator("li[data-connection]", { hasText: address });
      await expect(card).toHaveAttribute("data-status", "none");
      await expectAccessible(bob);

      await card.locator('[data-action="connect"]').click();
      await bob.waitForURL(`${toolStubURL()}/**`);
      await bob.locator("#username").fill(PEOPLE.bob.username);
      await bob.locator('button[data-action="allow"]').click();
      await expect(bob).toHaveURL(/\/settings\/accounts\?connected=/);
      await expect(bob.locator("[data-connected]")).toBeVisible();
      await expect(card).toHaveAttribute("data-status", "connected");
      await expect(card.locator("[data-scopes]")).toContainText("mcp");
      await expectAccessible(bob);

      await card.locator('[data-action="disconnect"]').click();
      await expectAccessible(bob);
      await card.locator('[data-action="confirm-disconnect"]').click();
      await expect(card).toHaveAttribute("data-status", "none");
    } finally {
      await context.close();
    }
  });
}

test("cancelling at the tool says so, and connects nothing @desktop", async ({ page, trackTool }) => {
  const address = await addTool(page, trackTool);
  await page.goto("/settings/accounts");
  const card = page.locator("li[data-connection]", { hasText: address });
  await card.locator('[data-action="connect"]').click();
  await page.waitForURL(`${toolStubURL()}/**`);
  await page.locator('button[data-action="deny"]').click();
  await expect(page).toHaveURL(/\/settings\/accounts\?error=denied/);
  await expect(page.locator("[data-connect-error]")).toBeVisible();
  await expect(card).toHaveAttribute("data-status", "none");
  await expectAccessible(page);
});
