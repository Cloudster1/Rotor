import { expect, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { archiveWorkflow, grantOnWorkflow, makeKnown, personID, plainWorkflow } from "../fixtures/access";
import { PEOPLE, signIn } from "../fixtures/auth";
import { confirmDialog, expectAccessible, openShell } from "../fixtures/shell";
import { createWorkflow } from "../fixtures/runs";

// A workflow's settings are pages of their own (#205): General, Versions, Sharing, Triggers and Tracing, beside the editor.

test("the editor keeps the YAML, the canvas and the checks, and links to the workflow's settings", async ({ page }) => {
  const name = `e2e-${randomUUID().slice(0, 12)}`;
  const id = await createWorkflow(page, plainWorkflow(name));
  try {
    await openShell(page, `/workflows/${id}`);
    await expect(page.locator("main h1")).toHaveText(name);
    await expect(page.locator("[data-yaml-editor]")).toBeVisible();
    await expect(page.locator("[data-problems]")).toContainText("No problems found.");
    for (const panel of ["[data-versions]", "[data-workflow-grants]", "[data-triggers]"]) {
      await expect(page.locator(panel)).toHaveCount(0);
    }
    await page.locator('[data-action="workflow-settings"]').click();
    await expect(page).toHaveURL(new RegExp(`/workflows/${id}/settings/general$`));
    await expect(page.locator("main h1")).toHaveText("General");
    await expect(page.locator('[data-settings-page="general"]')).toHaveAttribute("aria-current", "page");
  } finally {
    await archiveWorkflow(page, id);
  }
});

test("its administrator renames a workflow, and archives it with Undo", async ({ page }) => {
  const name = `e2e-${randomUUID().slice(0, 12)}`;
  const id = await createWorkflow(page, plainWorkflow(name));
  try {
    await openShell(page, `/workflows/${id}/settings`);
    await expect(page).toHaveURL(new RegExp(`/workflows/${id}/settings/general$`));
    const details = page.locator("[data-details]");
    await details.getByLabel("Name").fill(`${name}-renamed`);
    await details.getByLabel("Description").fill("Asks before it goes on.");
    await details.locator('[data-action="save-details"]').click();
    await expect(page.locator("[data-settings-of]")).toHaveAttribute("data-settings-of", `${name}-renamed`);
    await expectAccessible(page);

    const archive = page.locator("[data-archive]");
    await archive.locator('[data-action="archive-workflow"]').click();
    const status = archive.locator("[data-archive-status]");
    await expect(status).toContainText(`${name}-renamed is archived.`);
    await expectAccessible(page);
    await status.locator('[data-action="undo-archive"]').click();
    await expect(status).toContainText("is back in the list.");
    await expect(archive.locator('[data-action="archive-workflow"]')).toBeVisible();

    await openShell(page, "/workflows");
    await expect(page.locator(`[data-workflow="${name}-renamed"]`)).toContainText("Asks before it goes on.");
  } finally {
    await archiveWorkflow(page, id);
  }
});

test("archiving from the list asks first in a ConfirmDialog @desktop", async ({ page }) => {
  const name = `e2e-${randomUUID().slice(0, 12)}`;
  const id = await createWorkflow(page, plainWorkflow(name));
  try {
    await openShell(page, "/workflows");
    const row = page.locator(`[data-workflow="${name}"]`);
    await row.locator('[data-action="archive-workflow"]').click();
    await expect(page.locator("dialog[data-confirm]")).toContainText(`Archive ${name}?`);
    await expectAccessible(page);
    await confirmDialog(page);
    await expect(row).toHaveCount(0);
  } finally {
    await archiveWorkflow(page, id);
  }
});

test("a runner opens every section of a workflow's settings, read only but for triggers of their own @desktop", async ({ page, browser }) => {
  const name = `e2e-${randomUUID().slice(0, 12)}`;
  await makeKnown(browser, PEOPLE.carol);
  const id = await createWorkflow(page, plainWorkflow(name));
  const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  const carol = await context.newPage();
  try {
    await grantOnWorkflow(page, id, "runner", { userId: await personID(page, `${PEOPLE.carol.username}@suite.test`) });
    await signIn(carol, PEOPLE.carol, `/workflows/${id}/settings/general`);
    await expect(carol.locator("main h1")).toHaveText("General");
    await expect(carol.locator("[data-details]").getByLabel("Name")).toHaveAttribute("readonly", "");
    await expect(carol.locator('[data-read-only="details"]')).toBeVisible();
    await expect(carol.locator('[data-read-only="archive"]')).toBeVisible();
    await expect(carol.locator('[data-action="save-details"], [data-action="archive-workflow"]')).toHaveCount(0);
    await expectAccessible(carol);

    await carol.locator('[data-settings-page="versions"]').click();
    await expect(carol.locator("[data-versions] [data-version]")).toHaveCount(1);
    await carol.locator('[data-settings-page="sharing"]').click();
    await expect(carol.locator(`[data-workflow-grants] [data-grant][data-grantee="${PEOPLE.carol.name}"]`)).toContainText("Runner");
    await expect(carol.locator("[data-workflow-grants] [data-share-form]")).toHaveCount(0);
    await expectAccessible(carol);
    await carol.locator('[data-settings-page="triggers"]').click();
    await expect(carol.locator('[data-triggers] [data-action="add-trigger"]')).toBeVisible();

    // Once the grant goes, the same address tells carol the workflow is not shared with her.
    const revoked = await page.request.delete(`/api/v1/workflows/${id}/grants/${await carolsGrant(page, id)}`);
    expect(revoked.status()).toBe(204);
    await carol.reload();
    await expect(carol.locator("[data-settings-not-found]")).toBeVisible();
  } finally {
    await context.close();
    await archiveWorkflow(page, id);
  }
});

test("its administrator keeps no trace text of a workflow, and the choice survives a reload", async ({ page }) => {
  const name = `e2e-${randomUUID().slice(0, 12)}`;
  const id = await createWorkflow(page, plainWorkflow(name));
  try {
    await openShell(page, `/workflows/${id}/settings/general`);
    await page.locator('[data-settings-page="tracing"]').click();
    await expect(page).toHaveURL(new RegExp(`/workflows/${id}/settings/tracing$`));
    await expect(page.locator("main h1")).toHaveText("Tracing");
    await expect(page.locator("[data-tracing-installation]")).toBeVisible();
    await page.locator('[data-tracing-text] input[value="none"]').check();
    await page.locator('[data-action="save-tracing"]').click();
    await expect(page.locator("[role=status]", { hasText: "Tracing saved." })).toBeVisible();
    await expectAccessible(page);

    await page.reload();
    await expect(page.locator('[data-tracing-text] input[value="none"]')).toBeChecked();
    await expect(page.locator('[data-tracing-text] input[value="inherit"]')).not.toBeChecked();
    await expectAccessible(page);
  } finally {
    await archiveWorkflow(page, id);
  }
});

/** The grant carol holds on a workflow, as its grants list it. */
async function carolsGrant(page: Page, workflowID: string): Promise<string> {
  const listed = (await (await page.request.get(`/api/v1/workflows/${workflowID}/grants`)).json()) as { grants: { id: string; userName?: string }[] };
  const grant = listed.grants.find((each) => each.userName === PEOPLE.carol.name);
  expect(grant, "carol's grant").toBeDefined();
  return grant?.id ?? "";
}
