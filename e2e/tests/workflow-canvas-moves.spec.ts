import { expect, type Locator, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { expectAccessible } from "../fixtures/shell";

// The longest workflow Rotor ships, so the editor runs past the bottom of the window at every size.
const LONG_STARTER = "story-to-requirements";
const SIZES = [
  { tag: "@desktop", viewport: { width: 1440, height: 900 } },
  { tag: "@mobile", viewport: { width: 390, height: 844 } },
] as const;
const PHONE = SIZES[1].viewport;
const WHEEL_PX = 200;
const SWIPE_PX = 240;
const SWIPE_STEPS = 12;
const SWIPE_STEP_MS = 16;
// Room from the canvas's edge, clear of its border, of the toolbar along its top and of React Flow's credit at its foot.
const EDGE_PX = 40;

/** Opens the long starter on the canvas and waits until the checks have answered, whatever tools the stack holds. */
async function openLong(page: Page): Promise<void> {
  await page.goto(`/workflows/new?start=${LONG_STARTER}`);
  await expect(page.locator("[data-yaml-editor] .cm-content")).toContainText(`name: ${LONG_STARTER}`);
  await page.getByRole("tab", { name: "Canvas" }).click();
  await expect(page.locator("[data-canvas] .react-flow__node").first()).toBeVisible();
  await expect(page.locator("[data-problems]")).not.toBeEmpty();
  await expect(page.locator("[data-problems]")).not.toContainText("Checking...");
}

/** A workflow of one approval, named so that runs of this test never meet. */
function oneNode(): string {
  const lines = ["apiVersion: rotor.dev/v1", "kind: Workflow", "metadata:", `  name: e2e-${randomUUID().slice(0, 12)}`];
  return [...lines, "limits: { maxSteps: 3, timeout: 1m }", "entry: ask", "nodes:", "  - id: ask", "    type: approval", "    message: Go on?", ""].join("\n");
}

const main = (page: Page) => page.locator("main#main");
const scrolled = (page: Page) => main(page).evaluate((element) => element.scrollTop);
/** Where the graph sits and how large it is drawn, as React Flow writes it on its viewport. */
const transform = (page: Page) => page.locator("[data-canvas] .react-flow__viewport").evaluate((element) => (element as HTMLElement).style.transform);

async function boxOf(locator: Locator) {
  const box = await locator.boundingBox();
  if (!box) throw new Error("the element has no box");
  return box;
}

/** A point on the canvas's pane, low and to the right, within the window. */
async function onPane(page: Page, height: number) {
  const pane = await boxOf(page.locator("[data-canvas] .react-flow__pane"));
  return { x: pane.x + pane.width - EDGE_PX, y: Math.min(pane.y + pane.height - EDGE_PX, height - EDGE_PX) };
}

/** One finger dragged up the screen, as a touch screen sends it; Playwright's touchscreen only taps. */
async function swipeUp(page: Page, from: { x: number; y: number }): Promise<void> {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [from] });
  for (let i = 1; i <= SWIPE_STEPS; i++) {
    const y = from.y - (SWIPE_PX * i) / SWIPE_STEPS;
    await cdp.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: [{ x: from.x, y }] });
    await page.waitForTimeout(SWIPE_STEP_MS);
  }
  await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
  await cdp.detach();
}

test.describe("moving round the workflow canvas", () => {
  for (const { tag, viewport } of SIZES) {
    const size = `${viewport.width}x${viewport.height}`;
    test.describe(() => {
      test.use({ viewport });

      // React Flow once took every wheel over the canvas and did nothing with it, a dead zone in the page (#265).
      test(`a wheel over the canvas scrolls the page, Ctrl and the wheel zoom the graph, and the toolbar covers no node, at ${size} ${tag}`, async ({
        page,
      }) => {
        await openLong(page);
        const entry = page.locator('[data-canvas] .react-flow__node[data-layer="0"]');
        const toolbar = await boxOf(page.locator("[data-canvas-toolbar]"));
        expect((await boxOf(entry)).y).toBeGreaterThanOrEqual(toolbar.y + toolbar.height);

        const before = { top: await scrolled(page), graph: await transform(page) };
        const point = await onPane(page, viewport.height);
        await page.mouse.move(point.x, point.y);
        await page.mouse.wheel(0, WHEEL_PX);
        await expect.poll(() => scrolled(page)).toBeGreaterThan(before.top);
        expect(await transform(page)).toBe(before.graph);

        const top = await scrolled(page);
        const pane = await boxOf(page.locator("[data-canvas] .react-flow__pane"));
        await page.mouse.move(pane.x + pane.width / 2, pane.y + pane.height / 2);
        await page.keyboard.down("Control");
        await page.mouse.wheel(0, WHEEL_PX);
        await page.keyboard.up("Control");
        await expect.poll(() => transform(page)).not.toBe(before.graph);
        expect(await scrolled(page)).toBe(top);
        await expectAccessible(page);
      });

      // A finger on the canvas once panned the graph without end and never scrolled the page (#265).
      test(`a swipe over the canvas on a touch screen scrolls the page and leaves the graph, at ${size} @mobile`, async ({ page }) => {
        await openLong(page);
        const before = { top: await scrolled(page), graph: await transform(page) };
        await swipeUp(page, await onPane(page, viewport.height));
        await expect.poll(() => scrolled(page)).toBeGreaterThan(before.top);
        expect(await transform(page)).toBe(before.graph);
      });

      test(`a node added from the Add a node form is brought into view on the canvas, at ${size} ${tag}`, async ({ page }) => {
        await openLong(page);
        const add = page.locator("[data-add-node]");
        await add.getByLabel("Kind of node").selectOption("approval");
        await add.getByLabel("Node id").fill("extra");
        await add.getByLabel("Node id").press("Enter");
        const added = page.locator('[data-canvas] [data-node="extra"]');
        await expect(added).toBeVisible();
        await expect(page.locator("[data-inspector] select[name=selected-node]")).toHaveValue("extra");
        const canvas = await boxOf(page.locator("[data-canvas]"));
        // Again until it holds, since the graph glides to the new node.
        await expect(async () => {
          const node = await boxOf(added);
          expect(node.x).toBeGreaterThanOrEqual(canvas.x);
          expect(node.y).toBeGreaterThanOrEqual(canvas.y);
          expect(node.x + node.width).toBeLessThanOrEqual(canvas.x + canvas.width);
          expect(node.y + node.height).toBeLessThanOrEqual(canvas.y + canvas.height);
        }).toPass();
      });

      test(`a workflow of one node is drawn no larger than life, at ${size} ${tag}`, async ({ page }) => {
        await page.goto("/workflows/new");
        const editor = page.locator("[data-yaml-editor] .cm-content");
        await editor.click();
        await page.keyboard.press("ControlOrMeta+a");
        await page.keyboard.press("Delete");
        await page.keyboard.insertText(oneNode());
        await page.getByRole("tab", { name: "Canvas" }).click();
        await expect(page.locator('[data-canvas] [data-node="ask"]')).toBeVisible();
        await expect.poll(async () => Number(/scale\(([\d.]+)\)/.exec(await transform(page))?.[1])).toBeLessThanOrEqual(1);
      });
    });
  }

  test.describe(() => {
    test.use({ viewport: PHONE });

    // On a narrow screen the inspector stacks under the canvas, out of sight of the node just chosen (#265).
    test(`choosing a node on the canvas brings its inspector into view, at ${PHONE.width}x${PHONE.height} @mobile`, async ({ page }) => {
      await openLong(page);
      const inspector = page.locator("[data-inspector]");
      // The canvas ends where the window does, so the inspector under it starts out of sight.
      await main(page).evaluate((element) => {
        const canvas = document.querySelector("[data-canvas]");
        if (canvas) element.scrollBy(0, canvas.getBoundingClientRect().bottom - element.getBoundingClientRect().bottom);
      });
      await expect(inspector).not.toBeInViewport();

      const node = page.locator('[data-canvas] .react-flow__node[data-layer="1"]').first();
      const id = await node.getAttribute("data-node");
      await node.click();
      await expect(inspector.getByRole("heading").first()).toBeInViewport();
      await expect(inspector.locator("select[name=selected-node]")).toHaveValue(id ?? "");
      await expectAccessible(page);
    });
  });
});
