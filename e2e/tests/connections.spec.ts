import { expect, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { PEOPLE, signIn } from "../fixtures/auth";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { toolStubURL } from "../fixtures/stack";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** Adds a tool of its own at the double as alice, the administrator the default browser is signed in as. */
async function addTool(page: Page): Promise<string> {
  const address = `${toolStubURL()}/t/e2e-${randomUUID().slice(0, 12)}`;
  await page.goto("/settings");
  const form = page.locator("[data-add-tool]");
  await form.getByLabel("Address").fill(address);
  await form.locator('[data-action="add-tool"]').click();
  await expect(page.locator("[data-tools] li", { hasText: address })).toHaveAttribute("data-status", "ready");
  return address;
}

async function removeTool(page: Page, address: string): Promise<void> {
  await page.goto("/settings");
  const row = page.locator("[data-tools] li", { hasText: address });
  await row.locator('[data-action="remove-tool"]').click();
  await row.locator('[data-action="confirm-remove"]').click();
  await expect(row).toHaveCount(0);
}

for (const scheme of SCHEMES) {
  test(`a person connects at the tool's consent page, sees what was allowed and disconnects, in ${scheme} @desktop`, async ({ page, browser }) => {
    const address = await addTool(page);
    const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    const bob = await context.newPage();
    try {
      await startInScheme(bob, scheme);
      await signIn(bob, PEOPLE.bob, "/connections");
      const card = bob.locator("li[data-connection]", { hasText: address });
      await expect(card).toHaveAttribute("data-status", "none");
      await expectAccessible(bob);

      await card.locator('[data-action="connect"]').click();
      await bob.waitForURL(`${toolStubURL()}/**`);
      await bob.locator("#username").fill(PEOPLE.bob.username);
      await bob.locator('button[data-action="allow"]').click();
      await expect(bob).toHaveURL(/\/connections\?connected=/);
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
      await removeTool(page, address);
    }
  });
}

test("cancelling at the tool says so, and connects nothing @desktop", async ({ page }) => {
  const address = await addTool(page);
  try {
    await page.goto("/connections");
    const card = page.locator("li[data-connection]", { hasText: address });
    await card.locator('[data-action="connect"]').click();
    await page.waitForURL(`${toolStubURL()}/**`);
    await page.locator('button[data-action="deny"]').click();
    await expect(page).toHaveURL(/\/connections\?error=denied/);
    await expect(page.locator("[data-connect-error]")).toBeVisible();
    await expect(card).toHaveAttribute("data-status", "none");
    await expectAccessible(page);
  } finally {
    await removeTool(page, address);
  }
});
