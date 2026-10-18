import { expect, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";
import { toolStubURL } from "../fixtures/stack";

const SCHEMES: ColourScheme[] = ["light", "dark"];

for (const scheme of SCHEMES) {
  test(`an administrator adds a provider, tests it and removes it, in ${scheme} @desktop`, async ({ page }) => {
    // The tool double's stub model takes any key, on a tenant made by first use.
    const name = `e2e-${randomUUID().slice(0, 8)}`;
    await startInScheme(page, scheme);
    await page.goto("/settings");
    const section = page.locator("[data-providers]");
    await section.locator('[data-action="new-provider"]').click();
    const form = section.locator("[data-provider-form]");
    await form.getByLabel("Name", { exact: true }).fill(name);
    await form.getByLabel("Kind", { exact: true }).selectOption("openai_compatible");
    await form.getByLabel("Base URL", { exact: true }).fill(`${toolStubURL()}/t/${name}/llm/v1`);
    await form.getByLabel("Models", { exact: true }).fill("stub-model");
    await form.getByLabel("API key", { exact: true }).fill("e2e-key");
    await expectAccessible(page);
    await form.locator('[data-action="save-provider"]').click();

    const row = section.locator(`li[data-provider="${name}"]`);
    await expect(row).toContainText("Key held");
    await row.locator('[data-action="test-provider"]').click();
    await expect(row.locator('[data-test-result="ok"]')).toContainText("stub-model");
    await expectAccessible(page);

    await row.locator('[data-action="remove-provider"]').click();
    await row.locator('[data-action="confirm-remove-provider"]').click();
    await expect(row).toHaveCount(0);
  });
}
