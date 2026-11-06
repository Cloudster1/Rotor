import { expect, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { expectAccessible, startInScheme } from "../fixtures/shell";

const NAME = `E2E service ${randomUUID().slice(0, 8)}`;
const KEY_PREFIX = "rotor_ik_";

/** Revokes the key through the API, so a failed run leaves none behind; one already gone is fine. */
async function revokeKeyNamed(page: Page, name: string): Promise<void> {
  const listed = await page.request.get("/api/v1/settings/ingest-keys");
  if (!listed.ok()) return;
  const { keys } = (await listed.json()) as { keys: { id: string; name: string }[] };
  for (const key of keys.filter((each) => each.name === name)) {
    const gone = await page.request.delete(`/api/v1/settings/ingest-keys/${key.id}`);
    expect([204, 404], `revoking key ${name}`).toContain(gone.status());
  }
}

test.afterEach(async ({ page }) => {
  await revokeKeyNamed(page, NAME);
});

test("an administrator makes an ingest key and sees it once", async ({ page }) => {
  await startInScheme(page, "light");
  await page.goto("/admin/traces");
  const section = page.locator("[data-ingest-keys]");
  await expect(section.getByRole("button", { name: "Make a key" })).toBeVisible();

  await section.getByRole("button", { name: "Make a key" }).click();
  await section.getByLabel("Name", { exact: true }).fill(NAME);
  await expectAccessible(page);
  await section.getByRole("button", { name: "Make the key" }).click();

  const shown = section.getByLabel(`The key for ${NAME}`);
  await expect(shown).toBeVisible();
  const key = await shown.inputValue();
  expect(key.startsWith(KEY_PREFIX)).toBe(true);
  await expect(section).toContainText(`OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20${key}`);
  await expectAccessible(page);

  await section.getByRole("button", { name: "I have saved it" }).click();
  const row = section.locator(`[data-ingest-key="${NAME}"]`);
  await expect(row).toContainText(`Ends in ${key.slice(-4)}`);
  await expect(row).toContainText("Never used");
  await expect(page.locator("body")).not.toContainText(key);
  await expectAccessible(page);

  // The list after a reload holds the key's row and never the key.
  await page.reload();
  await expect(section.locator(`[data-ingest-key="${NAME}"]`)).toBeVisible();
  await expect(page.locator("body")).not.toContainText(KEY_PREFIX);

  await section.getByRole("button", { name: `Revoke ${NAME}` }).click();
  await section.getByRole("button", { name: "Revoke it" }).click();
  await expect(section.locator(`[data-ingest-key="${NAME}"]`)).toHaveCount(0);
});
