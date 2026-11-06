import { expect, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { expectAccessible, startInScheme } from "../fixtures/shell";

// The tool stub keeps what reaches /otlp/<sink>, and a sink name is lower case letters, digits and dashes.
const SUFFIX = randomUUID().replaceAll("-", "").slice(0, 8);
const NAME = `E2E tool ${SUFFIX}`;
const ENDPOINT = `http://tool-stub:8080/otlp/e2e-${SUFFIX}/v1/traces`;
const HEADER_NAME = "X-E2E-Key";
const HEADER_VALUE = `secret-${SUFFIX}`;
const TRACE_LINK = "http://localhost:16686/trace/{traceId}";

/** Takes the destination away through the API, so a failed run does not fill the most there can be; one already gone is fine. */
async function removeDestinationNamed(page: Page, name: string): Promise<void> {
  const listed = await page.request.get("/api/v1/settings/trace-destinations");
  if (!listed.ok()) return;
  const { destinations } = (await listed.json()) as { destinations: { id: string; name: string }[] };
  for (const destination of destinations.filter((each) => each.name === name)) {
    const gone = await page.request.delete(`/api/v1/settings/trace-destinations/${destination.id}`);
    expect([204, 404], `deleting destination ${name}`).toContain(gone.status());
  }
}

test.afterEach(async ({ page }) => {
  await removeDestinationNamed(page, NAME);
});

test("an administrator adds a trace destination", async ({ page }) => {
  await startInScheme(page, "light");
  await page.goto("/admin/traces");
  await expect(page.getByRole("heading", { level: 1, name: "Traces" })).toBeVisible();
  const section = page.locator("[data-trace-destinations]");
  await expect(section.getByRole("button", { name: "Add a destination" })).toBeVisible();
  await expectAccessible(page);

  await section.getByRole("button", { name: "Add a destination" }).click();
  await section.getByLabel("Name", { exact: true }).fill(NAME);
  await section.getByLabel("Endpoint", { exact: true }).fill(ENDPOINT);
  await section.getByRole("button", { name: "Add a header" }).click();
  await section.getByLabel("Header name 1").fill(HEADER_NAME);
  await section.getByLabel("Header value 1").fill(HEADER_VALUE);
  await section.getByLabel("Link to a trace", { exact: true }).fill(TRACE_LINK);
  await section.getByLabel("Send prompt, tool and answer text").check();
  await expectAccessible(page);

  // Text leaves only once the administrator says so.
  await section.getByRole("button", { name: "Save" }).click();
  await expect(section.getByRole("alert")).toContainText("Confirm that the text may leave Rotor");
  await section.getByLabel("I confirm that the text of runs may leave this installation for this destination.").check();
  await section.getByRole("button", { name: "Save" }).click();

  const row = section.locator(`[data-destination="${NAME}"]`);
  await expect(row).toBeVisible();
  await expect(row).toContainText(`Headers held: ${HEADER_NAME}`);
  await expect(row).toContainText(TRACE_LINK);
  await expect(row.locator('[data-destination-text="on"]')).toBeVisible();
  await expect(page.locator("body")).not.toContainText(HEADER_VALUE);
  await expectAccessible(page);

  await row.getByRole("button", { name: `Test ${NAME}` }).click();
  await expect(row.locator('[data-destination-test="delivered"]')).toBeVisible();

  await row.getByRole("button", { name: `Remove ${NAME}` }).click();
  await row.getByRole("button", { name: "Remove it" }).click();
  await expect(row).toHaveCount(0);
});
