import { expect, test } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { archiveWorkflow, makeKnown } from "../fixtures/access";
import { PEOPLE, signIn } from "../fixtures/auth";
import { createWorkflow } from "../fixtures/runs";
import { expectAccessible, openShell } from "../fixtures/shell";
import { archiveSkill, skillID, skillMD, skillName, zipOf } from "../fixtures/skills";

// The skills pages and the editor's skill picker (#223). Skills are never deleted, so each spec makes skills of
// names nobody used and archives them at the end.

/** A workflow whose one agent has no skills yet. */
function agentWorkflow(name: string): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    "metadata:",
    `  name: ${name}`,
    "limits:",
    "  maxSteps: 3",
    "  timeout: 1m",
    "entry: draft",
    "nodes:",
    "  - id: draft",
    "    type: agent",
    "    prompt: Word the story.",
    "",
  ].join("\n");
}

test("a person writes a skill, shares it with bob as Skill reader, and gives it to a workflow's agent with the picker @desktop", async ({ page, browser }) => {
  const name = skillName();
  await makeKnown(browser, PEOPLE.bob);
  const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  const bob = await context.newPage();
  let id = "";
  let workflowID = "";
  try {
    await openShell(page, "/skills");
    await expect(page.locator("main h1")).toHaveText("Skills");
    await page.locator('[data-action="new-skill"]').first().click();
    await expect(page.locator("main h1")).toHaveText("New skill");
    await expect(page.locator("[data-skill-clean]")).toBeVisible();
    await page.locator("[data-new-skill-md]").fill(skillMD(name).replace(/description:.*\n/, ""));
    await expect(page.locator('[data-skill-problems] [data-problem="skill_description"]')).toBeVisible();
    await expectAccessible(page);
    await page.locator("[data-new-skill-md]").fill(skillMD(name));
    await expect(page.locator("[data-skill-clean]")).toBeVisible();
    await page.getByLabel("Comment for version 1").fill("First");
    await page.locator('[data-action="create-skill"]').click();
    await expect(page).toHaveURL(/\/skills\/[0-9a-f-]{36}\/files$/);
    id = await skillID(page, name);
    await expect(page.locator('[data-skill-content="SKILL.md"]')).toContainText("Write the story in one sentence.");
    await expectAccessible(page);

    await page.locator('[data-settings-page="sharing"]').click();
    const grants = page.locator("[data-skill-grants]");
    const form = grants.locator("[data-share-form]");
    await form.locator("[data-person-search]").fill(PEOPLE.bob.username);
    await expect(form.locator("[data-share-who] option", { hasText: PEOPLE.bob.name })).toHaveCount(1);
    await form.locator("[data-share-who]").selectOption({ label: PEOPLE.bob.name });
    await form.locator("[data-share-role]").selectOption({ label: "Skill reader" });
    await form.locator('[data-action="share"]').click();
    await expect(grants.locator(`[data-grant][data-grantee="${PEOPLE.bob.name}"]`)).toContainText("Skill reader");
    await expectAccessible(page);

    // Bob reads it now, and only reads it.
    await signIn(bob, PEOPLE.bob, `/skills/${id}/files`);
    await expect(bob.locator('[data-skill-content="SKILL.md"]')).toContainText("Write the story in one sentence.");
    await expect(bob.locator('[data-read-only="files"]')).toBeVisible();
    await expect(bob.locator('[data-action="edit-files"]')).toHaveCount(0);

    workflowID = await createWorkflow(page, agentWorkflow(`e2e-${randomUUID().slice(0, 12)}`));
    await openShell(page, `/workflows/${workflowID}`);
    const picker = page.locator("[data-skill-picker]");
    await picker.locator("[data-picker-skill]").selectOption({ label: name });
    await expect(picker.locator("[data-picker-version]")).toHaveValue("1");
    await page.locator("[data-yaml-editor] .cm-line", { hasText: "kind: Workflow" }).click();
    await picker.locator('[data-action="insert-skill"]').click();
    await expect(picker.locator("[data-picker-said]")).toHaveText("Put the cursor inside an agent node in the YAML first.");
    await page.locator("[data-yaml-editor] .cm-line", { hasText: "prompt: Word the story." }).click();
    await picker.locator('[data-action="insert-skill"]').click();
    await expect(picker.locator("[data-picker-said]")).toHaveText(`Added ${name}, version 1, to draft.`);
    await expect(page.locator("[data-yaml-editor] .cm-line", { hasText: `name: ${name}, version: 1` })).toBeVisible();
    await expect(page.locator("[data-problems]")).toContainText("No problems found.");
    await expectAccessible(page);

    await page.getByRole("tab", { name: "Canvas" }).click();
    const inspector = page.locator("[data-inspector]");
    await inspector.locator('select[name="selected-node"]').selectOption("draft");
    await expect(inspector.locator(`[data-node-skill="${name}"]`)).toContainText(`${name}, version 1`);
    await page.getByRole("tab", { name: "YAML" }).click();

    await page.locator('[data-action="save-workflow"]').click();
    // The editor starts over on the version it saved, so the saved version is asked of the API.
    const current = async () => (await (await page.request.get(`/api/v1/workflows/${workflowID}`)).json()) as { version: { number: number; specYAML: string } };
    await expect.poll(async () => (await current()).version.number).toBe(2);
    expect((await current()).version.specYAML).toMatch(new RegExp(`    skills:\\n      - \\{ ?name: ${name}, version: 1 ?\\}\\n`));
  } finally {
    await context.close();
    if (workflowID) await archiveWorkflow(page, workflowID);
    await archiveSkill(page, id);
  }
});

test("a person imports a skill from a zip, imports it again as its next version, and exports that version @desktop", async ({ page }) => {
  const name = skillName();
  const folder = (body: string) => zipOf({ [`${name}/SKILL.md`]: skillMD(name, body), [`${name}/references/terms.md`]: "Story: what a person wants.\n" });
  let id = "";
  try {
    await openShell(page, "/skills/import");
    await expect(page.locator("main h1")).toHaveText("Import a skill");
    const result = page.locator("[data-import-result]");
    await page.locator("[data-import-zip]").setInputFiles({ name: `${name}.zip`, mimeType: "application/zip", buffer: folder("First.") });
    await page.getByLabel("Comment for the version").fill("Imported");
    await page.locator('[data-action="import-skill"]').click();
    await expect(result).toHaveAttribute("data-import-result", "created");
    await expect(result).toContainText(`Made the skill ${name}, version 1.`);
    id = await skillID(page, name);
    await expectAccessible(page);

    await page.locator("[data-import-zip]").setInputFiles({ name: `${name}.zip`, mimeType: "application/zip", buffer: folder("Second.") });
    await page.locator('[data-action="import-skill"]').click();
    await expect(result).toHaveAttribute("data-import-result", "added");
    await expect(result).toContainText(`Saved version 2 of ${name}`);

    // A hidden file is refused at its path, as the library's path rule says.
    await page.locator("[data-import-zip]").setInputFiles({
      name: "bad.zip",
      mimeType: "application/zip",
      buffer: zipOf({ "SKILL.md": skillMD(name), ".DS_Store": "x" }),
    });
    await page.locator('[data-action="import-skill"]').click();
    await expect(page.locator('[data-import-problems] [data-problems-at=".DS_Store"]')).toBeVisible();
    await expectAccessible(page);

    await openShell(page, `/skills/${id}/versions`);
    await page.locator('[data-action="compare"]').click();
    await expect(page.locator('[data-diff-file="SKILL.md"]')).toHaveAttribute("data-status", "changed");
    await expect(page.locator('[data-diff-file="SKILL.md"] [data-op="insert"]')).toContainText("Second.");
    await expectAccessible(page);

    const download = page.waitForEvent("download");
    await page.locator('[data-version="2"] [data-action="export-version"]').click();
    const file = await download;
    expect(file.suggestedFilename()).toBe(`${name}-v2.zip`);
    const bytes = await readFile(await file.path());
    expect(bytes.subarray(0, 2).toString()).toBe("PK");
    expect(bytes.includes(Buffer.from(`${name}/SKILL.md`))).toBe(true);
  } finally {
    await archiveSkill(page, id);
  }
});
