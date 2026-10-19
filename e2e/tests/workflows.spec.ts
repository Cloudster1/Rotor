import { expect, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { PEOPLE, signIn } from "../fixtures/auth";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/** A workflow that binds no tool, so it saves whatever tools the shared stack has. */
function workflow(name: string, message: string, withLimits = true): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    "metadata:",
    `  name: ${name}`,
    withLimits ? "limits: {maxSteps: 3, timeout: 1m}" : "",
    "entry: ask",
    "nodes:",
    "  - id: ask",
    "    type: approval",
    `    message: ${message}`,
    "",
  ].join("\n");
}

/** Replaces everything in the editor, as typing over a selection would. */
async function replaceText(page: Page, text: string): Promise<void> {
  const editor = page.locator("[data-yaml-editor] .cm-content");
  await editor.click();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.press("Delete");
  await page.keyboard.insertText(text);
}

for (const scheme of SCHEMES) {
  test(`a person writes a workflow, sees what is wrong, saves two versions, compares and shares them, in ${scheme} @desktop`, async ({ page }) => {
    const name = `e2e-${randomUUID().slice(0, 12)}`;
    await startInScheme(page, scheme);
    await page.goto("/workflows/new");
    await expect(page.locator("[data-yaml-editor] .cm-content")).toBeVisible();

    await replaceText(page, workflow(name, "Go on?", false));
    const problem = page.locator("[data-problems] [data-problem]").first();
    await expect(problem).toContainText("limits");
    await expect(page.locator(".cm-lint-marker-error, .cm-lintRange-error").first()).toBeVisible();
    await expectAccessible(page);

    await replaceText(page, workflow(name, "Go on?"));
    await expect(page.locator("[data-problems]")).toContainText("No problems found.");
    await page.locator('[data-action="save-workflow"]').click();
    await expect(page).toHaveURL(/\/workflows\/[0-9a-f-]{36}$/);
    await expect(page.locator("main h1")).toHaveText(name);

    await replaceText(page, workflow(name, "Really go on?"));
    await page.getByLabel("What changed").fill("Asks more firmly");
    await page.locator('[data-action="save-workflow"]').click();
    await expect(page.locator("[data-versions] [data-version]")).toHaveCount(2);

    await page.locator('[data-action="compare"]').click();
    await expect(page.locator('[data-diff] [data-op="insert"]')).toContainText("Really go on?");
    await expect(page.locator('[data-diff] [data-op="delete"]')).toContainText("Go on?");

    const share = page.locator("[data-share]");
    await share.getByLabel("Groups I am in").check();
    await share.getByLabel("engineering").check();
    await share.locator('[data-action="save-sharing"]').click();
    await expect(share.getByRole("status")).toBeVisible();
    await expectAccessible(page);
  });
}

test("a workflow shared with a group is seen by its members, read only, and by nobody else @desktop", async ({ page, browser }) => {
  const name = `e2e-${randomUUID().slice(0, 12)}`;
  await page.goto("/workflows/new");
  await replaceText(page, workflow(name, "Go on?"));
  await expect(page.locator("[data-problems]")).toContainText("No problems found.");
  await page.locator('[data-action="save-workflow"]').click();
  await expect(page).toHaveURL(/\/workflows\/[0-9a-f-]{36}$/);
  const address = new URL(page.url()).pathname;
  const share = page.locator("[data-share]");
  await share.getByLabel("Groups I am in").check();
  await share.getByLabel("engineering").check();
  await share.locator('[data-action="save-sharing"]').click();
  await expect(share.getByRole("status")).toBeVisible();

  for (const [person, sees] of [
    [PEOPLE.bob, true],
    [PEOPLE.carol, false],
  ] as const) {
    const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
    const other = await context.newPage();
    try {
      await signIn(other, person, address);
      if (sees) {
        await expect(other.locator("main h1")).toHaveText(name);
        await expect(other.locator("[data-read-only]")).toBeVisible();
        await expect(other.locator('[data-action="save-workflow"]')).toHaveCount(0);
        await expectAccessible(other);
      } else {
        await expect(other.getByRole("alert")).toContainText("not shared with you");
      }
    } finally {
      await context.close();
    }
  }
});

/** Reads from a tool and then writes through it, with no approval between; short lines, so the editor never scrolls sideways. */
function readThenWrite(name: string): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    `metadata: {name: ${name}}`,
    "tools:",
    "  r:",
    "    tool: e2e-no-such-tool",
    "    scopes: [mcp:read]",
    "    allowTools: [get_requirement]",
    "  w:",
    "    tool: e2e-no-such-tool",
    "    scopes: [mcp]",
    "    access: write",
    "    allowTools: [create_requirement]",
    "limits: {maxSteps: 5, timeout: 1m}",
    "entry: read",
    "nodes:",
    "  - id: read",
    "    type: tool",
    "    tool: r",
    "    call: get_requirement",
    "    arguments: {key: R-1}",
    "  - id: write",
    "    type: tool",
    "    tool: w",
    "    call: create_requirement",
    '    arguments: {title: "{{ nodes.read.title }}"}',
    "edges:",
    "  - {from: read, to: write}",
    "",
  ].join("\n");
}

for (const scheme of SCHEMES) {
  test(`the editor warns about a write that what it read could steer, in ${scheme} @desktop`, async ({ page }) => {
    await startInScheme(page, scheme);
    await page.goto("/workflows/new");
    await expect(page.locator("[data-yaml-editor] .cm-content")).toBeVisible();
    await replaceText(page, readThenWrite(`e2e-${randomUUID().slice(0, 12)}`));
    const warnings = page.locator("[data-problems] [data-warnings]");
    await expect(warnings.locator('[data-warning="untrusted_then_write"]')).toContainText("Put an approval node between them");
    await expect(warnings).toContainText("Saving still works");
    await expect(page.locator(".cm-lint-marker-warning, .cm-lintRange-warning").first()).toBeVisible();
    await expectAccessible(page);
  });
}
