import { expect, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** A workflow of one approval, named so that runs of this test never meet. */
function workflow(name: string): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    "# Drawn on the canvas from here on.",
    "metadata:",
    `  name: ${name}`,
    "limits: { maxSteps: 3, timeout: 1m }",
    "entry: ask",
    "nodes:",
    "  - id: ask",
    "    type: approval",
    "    message: Go on?",
    "",
  ].join("\n");
}

async function replaceText(page: Page, text: string): Promise<void> {
  const editor = page.locator("[data-yaml-editor] .cm-content");
  await editor.click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.press("Delete");
  await page.keyboard.insertText(text);
}

/** The YAML editor's text, line by line as CodeMirror draws it. */
async function yamlLines(page: Page): Promise<string[]> {
  return page.locator("[data-yaml-editor] .cm-line").allInnerTexts();
}

for (const scheme of SCHEMES) {
  test(`a person draws a workflow on the canvas with the keyboard, fixes what is wrong and saves the YAML they would have written, in ${scheme} @desktop`, async ({
    page,
  }) => {
    const name = `e2e-${randomUUID().slice(0, 12)}`;
    await startInScheme(page, scheme);
    await page.goto("/workflows/new");
    await replaceText(page, workflow(name));
    await expect(page.locator("[data-problems]")).toContainText("No problems found.");

    await page.getByRole("tab", { name: "YAML" }).focus();
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("tab", { name: "Canvas" })).toHaveAttribute("aria-selected", "true");
    const canvas = page.locator("[data-canvas]");
    await expect(canvas.locator('[data-node="ask"]')).toBeVisible();

    const add = page.locator("[data-add-node]");
    await add.getByLabel("Kind of node").selectOption("approval");
    await add.getByLabel("Node id").focus();
    await page.keyboard.type("confirm");
    await page.keyboard.press("Enter");
    await expect(canvas.locator('[data-node="confirm"]')).toBeVisible();
    await expect(canvas.locator('[data-node="confirm"] [data-node-badge]')).toBeVisible();
    await expectAccessible(page);

    const inspector = page.locator("[data-inspector]");
    await inspector.getByLabel("Message to the approver").focus();
    await page.keyboard.type("Sure?");
    await inspector.getByLabel("Node to edit").selectOption("ask");
    await inspector.getByLabel("Add a next node").selectOption("confirm");
    await inspector.getByRole("button", { name: "Add", exact: true }).focus();
    await page.keyboard.press("Enter");
    await expect(page.locator("[data-problems]")).toContainText("No problems found.");
    await expect(canvas.locator("[data-node-badge]")).toHaveCount(0);
    await expectAccessible(page);

    await page.getByRole("tab", { name: "Canvas" }).focus();
    await page.keyboard.press("ArrowLeft");
    const drawn = await yamlLines(page);
    expect(drawn).toContain("# Drawn on the canvas from here on.");
    expect(drawn).toContain("limits: { maxSteps: 3, timeout: 1m }");
    expect(drawn).toContain("edges:");
    expect(drawn).toContain("  - { from: ask, to: confirm }");

    await page.locator('[data-action="save-workflow"]').click();
    await expect(page).toHaveURL(/\/workflows\/[0-9a-f-]{36}$/);
    await page.reload();
    await expect(page.locator("main h1")).toHaveText(name);
    await expect(page.locator("[data-yaml-editor] .cm-content")).toBeVisible();
    expect(await yamlLines(page)).toEqual(drawn);
  });
}
