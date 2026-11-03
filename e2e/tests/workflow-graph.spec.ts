import { expect, type Locator, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { expectAccessible, startInScheme } from "../fixtures/shell";

// Enough steps that React Flow sees a drag, not a click, and finds the handle under the pointer.
const DRAG_STEPS = 12;
// Longer than the canvas's glide to a new node, so two looks this far apart agree once it has stopped.
const SETTLE_MS = 250;

/** Three approvals nothing connects yet, so every way between them is drawn on the graph. */
function workflow(name: string): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    "metadata:",
    `  name: ${name}`,
    "inputs: { type: object, properties: { kind: { type: string } } }",
    "limits: { maxSteps: 10, timeout: 1m }",
    "entry: ask",
    "nodes:",
    "  - id: ask",
    "    type: approval",
    "    message: Go on?",
    "  - id: done",
    "    type: approval",
    "    message: Done?",
    "  - id: later",
    "    type: approval",
    "    message: Later?",
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

async function yamlLines(page: Page): Promise<string[]> {
  return page.locator("[data-yaml-editor] .cm-line").allInnerTexts();
}

/** Where an element's middle is once the canvas has stopped gliding to a node it brings into view. */
async function centre(at: Locator): Promise<{ x: number; y: number }> {
  let last = "";
  await expect
    .poll(async () => {
      const now = JSON.stringify(await at.boundingBox());
      const still = now === last;
      last = now;
      return still;
    }, { intervals: [SETTLE_MS] })
    .toBe(true);
  const box = await at.boundingBox();
  if (!box) throw new Error("not drawn");
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
}

/** Drags a way out of one node's bottom handle and lets go at a point or on another node's top handle. */
async function drag(page: Page, from: Locator, to: Locator | { x: number; y: number }): Promise<void> {
  const start = await centre(from);
  const end = "x" in to ? to : await centre(to);
  await page.mouse.move(start.x, start.y);
  await page.mouse.down();
  await page.mouse.move(end.x, end.y, { steps: DRAG_STEPS });
  await page.mouse.up();
}

test("a person draws a workflow on the graph by dragging ways and adding nodes, removes and undoes, and saves it @desktop", async ({ page }) => {
  const name = `e2e-${randomUUID().slice(0, 12)}`;
  await startInScheme(page, "light");
  await page.goto("/workflows/new");
  await replaceText(page, workflow(name));
  await page.getByRole("tab", { name: "Canvas" }).click();
  const canvas = page.locator("[data-canvas]");
  const node = (id: string) => canvas.locator(`[data-node="${id}"]`);
  const out = (id: string) => node(id).locator(".react-flow__handle.source");
  const into = (id: string) => node(id).locator(".react-flow__handle.target");
  const way = (from: string, to: string) => canvas.locator(`[aria-label^="Way from ${from} to ${to}"]`);
  await expect(node("later")).toBeVisible();

  // A way dragged from one node onto another connects them.
  await drag(page, out("ask"), into("done"));
  await expect(way("ask", "done")).toHaveCount(1);

  // A way dragged into the open asks what to add there, and the new node comes connected.
  const doneAt = await centre(node("done"));
  await drag(page, out("done"), { x: doneAt.x + 260, y: doneAt.y + 40 });
  const picker = page.locator("[data-node-picker]");
  await expect(picker).toBeVisible();
  await expectAccessible(page);
  await picker.getByRole("button", { name: /^Agent/ }).click();
  await expect(node("agent")).toBeVisible();
  await expect(way("done", "agent")).toHaveCount(1);

  // The "+" under a node adds the next one after it.
  await node("agent").hover();
  await node("agent").locator("[data-node-add]").click();
  await picker.getByRole("button", { name: /^Router/ }).click();
  await expect(way("agent", "router")).toHaveCount(1);
  await node("router").locator("[data-node-add]").click();
  await picker.getByRole("button", { name: /^Approval/ }).click();
  await expect(way("router", "approval")).toHaveCount(1);

  // A router connected by hand asks for the case it goes there by.
  await drag(page, out("router"), into("later"));
  const cases = page.locator("[data-case-dialog]");
  await expect(cases).toBeVisible();
  await cases.getByLabel("When the value is").fill("later");
  await cases.locator("[data-case-add]").click();
  await expect(way("router", "later")).toHaveCount(1);

  // A selected way goes with Delete, and Ctrl+Z brings it back.
  await way("ask", "done").focus();
  await page.keyboard.press("Enter");
  await page.keyboard.press("Delete");
  await expect(way("ask", "done")).toHaveCount(0);
  await page.keyboard.press("ControlOrMeta+z");
  await expect(way("ask", "done")).toHaveCount(1);

  // Escape closes the picker without adding anything.
  await page.locator("[data-canvas-toolbar]").getByRole("button", { name: "Add node" }).click();
  await expect(picker).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(picker).toHaveCount(0);

  const inspector = page.locator("[data-inspector]");
  await inspector.getByLabel("Node to edit").selectOption("agent");
  await inspector.getByLabel("Prompt").fill("Sort the request.");
  await inspector.getByLabel("Node to edit").selectOption("router");
  await inspector.getByLabel("Value to route on").fill("inputs.kind");
  await inspector.getByLabel("Node to edit").selectOption("approval");
  await inspector.getByLabel("Message to the approver").fill("Fine?");
  await expect(page.locator("[data-problems]")).toContainText("No problems found.");

  await page.getByRole("tab", { name: "YAML" }).click();
  const drawn = await yamlLines(page);
  for (const line of ["  - { from: ask, to: done }", "  - { from: done, to: agent }", "  - { from: agent, to: router }", "    default: approval"]) {
    expect(drawn).toContain(line);
  }
  expect(drawn.join("\n")).toMatch(/later: later/);
  await page.locator('[data-action="save-workflow"]').click();
  await expect(page).toHaveURL(/\/workflows\/[0-9a-f-]{36}$/);
  await page.reload();
  await expect(page.locator("[data-yaml-editor] .cm-content")).toBeVisible();
  expect(await yamlLines(page)).toEqual(drawn);
});
