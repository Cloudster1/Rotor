import { expect, type Locator, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { archiveWorkflow, deleteGroupNamed, deleteRoleNamed, makeKnown, plainWorkflow } from "../fixtures/access";
import { PEOPLE, signIn } from "../fixtures/auth";
import { createWorkflow } from "../fixtures/runs";
import { expectAccessible, openShell, scrollsSideways } from "../fixtures/shell";

// Admin's Access page (#203): grants on the Roles tab, the matrix and the groups, each spec cleaning up what it made.

/** Picks a person in a picker by typing part of their name, then choosing them from what the server found. */
async function pickPerson(scope: Locator, select: Locator, name: string): Promise<void> {
  await scope.locator("[data-person-search]").fill(name.split(" ")[0]!.toLowerCase());
  await expect(select.locator("option", { hasText: name })).toHaveCount(1);
  await select.selectOption({ label: name });
}

/** Confirms the dialog open on the page. */
async function confirm(page: Page): Promise<void> {
  const dialog = page.locator("dialog[data-confirm]");
  await expect(dialog).toBeVisible();
  await dialog.locator('[data-action="confirm"]').click();
  await expect(dialog).toHaveCount(0);
}

test("an administrator grants a role to a person and to a group on the Roles tab, and revokes them @desktop", async ({ page, browser }) => {
  const name = `e2e-${randomUUID().slice(0, 12)}`;
  await makeKnown(browser, PEOPLE.bob);
  const workflowID = await createWorkflow(page, plainWorkflow(name));
  try {
    await openShell(page, "/admin/access");
    const panel = page.locator('[data-access-tab="roles"]');
    await expect(page.getByRole("tab", { name: "Roles" })).toHaveAttribute("aria-selected", "true");
    const form = panel.locator("[data-grant-form]");
    const who = form.locator("[data-grant-who]");

    await pickPerson(form, who, PEOPLE.bob.name);
    await form.locator("[data-grant-role]").selectOption({ label: "Runner" });
    await form.locator("[data-grant-where]").selectOption({ label: name });
    await form.locator('[data-action="grant"]').click();
    // A grant made empties the choice of whom, so the next is picked only once the first is in.
    await expect(who).toHaveValue("");

    await who.selectOption({ label: "engineering" });
    await form.locator("[data-grant-role]").selectOption({ label: "Editor" });
    await form.locator("[data-grant-where]").selectOption({ label: name });
    await form.locator('[data-action="grant"]').click();

    await panel.locator("[data-assignment-search]").fill(name);
    const bob = panel.locator(`[data-assignment][data-grantee="${PEOPLE.bob.name}"]`);
    const engineering = panel.locator('[data-assignment][data-grantee="engineering"]');
    // The search applies after a pause, so the rows are awaited before any is read: the two grants, and the
    // creator's own Workflow administrator grant on the new workflow.
    await expect(panel.locator("[data-assignment]")).toHaveCount(3);
    await expect(panel.locator('[data-assignment][data-role="workflow_administrator"]')).toHaveCount(1);
    await expect(bob).toContainText("Runner");
    await expect(bob).toContainText(name);
    await expect(engineering).toContainText("Editor");
    await expectAccessible(page);

    await panel.locator("[data-assignment-role]").selectOption({ label: "Editor" });
    await expect(panel.locator("[data-assignment]")).toHaveCount(1);
    await panel.locator("[data-assignment-role]").selectOption({ label: "Every role" });

    for (const row of [bob, engineering]) {
      await row.locator('[data-action="revoke"]').click();
      await expectAccessible(page);
      await confirm(page);
      await expect(row).toHaveCount(0);
    }
  } finally {
    await archiveWorkflow(page, workflowID);
  }
});

test("a role made in the matrix keeps a ticked permission after a reload, and is deleted after asking @desktop", async ({ page }) => {
  const name = `e2e-role-${randomUUID().slice(0, 8)}`;
  try {
    await openShell(page, "/admin/access?tab=matrix");
    await expect(page.getByRole("tab", { name: "Matrix" })).toHaveAttribute("aria-selected", "true");
    await page.locator('[data-action="new-role"]').click();
    const dialog = page.locator("dialog[data-new-role]");
    await dialog.getByLabel("Role name").fill(name);
    await dialog.getByLabel("What it is for").fill("Made by the e2e suite.");
    await expectAccessible(page);
    await dialog.locator('[data-action="save-role"]').click();
    await expect(dialog).toHaveCount(0);

    const sees = page.getByLabel(`${name} may see a workflow, its versions and what changed between them`);
    const runs = page.getByLabel(`${name} may start runs of a workflow and follow their own`);
    await expect(sees).toBeChecked();
    await expect(runs).not.toBeChecked();
    // The box follows the saved role, so it shows the tick once the server has it.
    await runs.click();
    await expect(runs).toBeChecked();
    await expect(runs).toBeEnabled();
    await page.reload();
    await expect(runs).toBeChecked();
    await expect(page.getByLabel("Administrator may administer Rotor", { exact: true })).toBeDisabled();
    await expectAccessible(page);

    const column = page.locator("[data-role-column]", { hasText: name });
    await column.locator('[data-action="role-menu"]').click();
    await column.locator('[data-action="delete-role"]').click();
    await confirm(page);
    await expect(column).toHaveCount(0);
  } finally {
    await deleteRoleNamed(page, name);
  }
});

test("a group made by hand gains a member and loses them again @desktop", async ({ page, browser }) => {
  const name = `e2e-group-${randomUUID().slice(0, 8)}`;
  await makeKnown(browser, PEOPLE.bob);
  try {
    await openShell(page, "/admin/access?tab=groups");
    const panel = page.locator('[data-access-tab="groups"]');
    await expect(panel.locator("[data-group]").first()).toHaveAttribute("data-group-builtin", "everyone");
    await expect(panel.locator('[data-group="engineering"]')).toHaveAttribute("data-group-source", "oidc");

    const form = panel.locator("[data-new-group]");
    await form.getByLabel("New group").fill(name);
    await form.locator('[data-action="create-group"]').click();
    const card = panel.locator(`[data-group="${name}"]`);
    await expect(card).toBeVisible();
    await expect(card).toContainText("Nobody in it yet.");

    await pickPerson(card, card.locator("[data-group-add]"), PEOPLE.bob.name);
    await card.locator('[data-action="add-member"]').click();
    const member = card.locator(`[data-member="${PEOPLE.bob.name}"]`);
    await expect(member).toBeVisible();
    await expectAccessible(page);

    await member.locator('[data-action="remove-member"]').click();
    await confirm(page);
    await expect(member).toHaveCount(0);

    await card.locator('[data-action="delete-group"]').click();
    await confirm(page);
    await expect(card).toHaveCount(0);
  } finally {
    await deleteGroupNamed(page, name);
  }
});

test("on a phone each tab of Access fits the screen, and the tabs are walked with the keyboard @mobile", async ({ page }) => {
  await openShell(page, "/admin/access");
  for (const tab of ["Roles", "Matrix", "Groups"]) {
    await page.getByRole("tab", { name: tab }).click();
    await expect(page.locator(`[data-access-tab="${tab.toLowerCase()}"]`)).toBeVisible();
    expect(await scrollsSideways(page), tab).toBe(false);
  }
  await page.getByRole("tab", { name: "Groups" }).focus();
  await page.keyboard.press("Home");
  await expect(page.getByRole("tab", { name: "Roles" })).toBeFocused();
  await expect(page).toHaveURL(/\/admin\/access$/);
});

test("a workflow shared with a person through its grants is seen by them, read only, until the grant goes @desktop", async ({ page, browser }) => {
  const name = `e2e-${randomUUID().slice(0, 12)}`;
  await makeKnown(browser, PEOPLE.carol);
  const workflowID = await createWorkflow(page, plainWorkflow(name));
  const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  const carol = await context.newPage();
  try {
    await openShell(page, `/workflows/${workflowID}`);
    const grants = page.locator("[data-workflow-grants]");
    const form = grants.locator("[data-share-form]");
    await pickPerson(form, form.locator("[data-share-who]"), PEOPLE.carol.name);
    await form.locator("[data-share-role]").selectOption({ label: "Runner" });
    await form.locator('[data-action="share"]').click();
    const grant = grants.locator(`[data-grant][data-grantee="${PEOPLE.carol.name}"]`);
    await expect(grant).toContainText("Runner");
    await expectAccessible(page);

    await signIn(carol, PEOPLE.carol, `/workflows/${workflowID}`);
    await expect(carol.locator("main h1")).toHaveText(name);
    await expect(carol.locator("[data-read-only]")).toBeVisible();
    await expect(carol.locator('[data-action="run-workflow"]')).toBeVisible();
    await expect(carol.locator(`[data-workflow-grants] [data-grant][data-grantee="${PEOPLE.carol.name}"]`)).toBeVisible();
    await expect(carol.locator("[data-workflow-grants] [data-share-form]")).toHaveCount(0);
    await expectAccessible(carol);

    await grant.locator('[data-action="remove-grant"]').click();
    await confirm(page);
    await expect(grant).toHaveCount(0);
    await carol.reload();
    await expect(carol.getByRole("alert")).toContainText("not shared with you");
  } finally {
    await context.close();
    await archiveWorkflow(page, workflowID);
  }
});
