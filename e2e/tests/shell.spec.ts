import { expect, test } from "@playwright/test";
import { buildVersion } from "../fixtures/stack";
import { expectAccessible, openShell, PLACES, scrollsSideways } from "../fixtures/shell";

test("the index opens the first place", async ({ page }) => {
  await openShell(page, "/");
  await expect(page).toHaveURL(/\/workflows$/);
  await expect(page.getByRole("heading", { level: 1, name: "Workflows" })).toBeVisible();
});

test("the sidebar leads to every place and marks the one open", async ({ page }) => {
  await openShell(page, "/workflows");
  const nav = page.getByRole("navigation", { name: "Places" });
  for (const place of PLACES) {
    await nav.getByRole("link", { name: place.label }).click();
    await expect(page).toHaveURL(new RegExp(`${place.opens}$`));
    await expect(page.getByRole("heading", { level: 1, name: place.heading })).toBeVisible();
    // By its place, not its text: the Approvals link also carries a badge of what waits for the person.
    await expect(nav.locator('[aria-current="page"]')).toHaveAttribute("data-place", place.path);
  }
});

test("a chosen colour scheme survives a reload", async ({ page }) => {
  await openShell(page, "/runs");
  const button = page.locator('[data-action="theme"]');
  await expect(page.locator("html")).not.toHaveAttribute("data-theme");
  await button.click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await button.click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.reload();
  await expect(page.locator("main#main")).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(button).toHaveAttribute("data-theme-choice", "dark");
});

test("the settings name the build the server runs, through nginx and the api", async ({ page }) => {
  await openShell(page, "/settings/account");
  const version = page.locator("[data-build-version]");
  await expect(version).toBeVisible();
  const expected = buildVersion();
  if (expected) await expect(version).toHaveText(expected);
});

test("the skip link moves focus to the content", async ({ page }) => {
  await openShell(page, "/approvals");
  await page.keyboard.press("Tab");
  const skip = page.locator("[data-skip-link]");
  await expect(skip).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.locator("main#main")).toBeFocused();
});

test("a narrow screen never scrolls sideways @mobile", async ({ page }) => {
  for (const place of PLACES) {
    await openShell(page, place.path);
    expect(await scrollsSideways(page), place.path).toBe(false);
  }
});

// The longest workflow Rotor ships, so the editor runs far past the bottom of the window.
const LONG_STARTER = "story-to-requirements";
const SIZES = [
  { tag: "@desktop", viewport: { width: 1440, height: 900 } },
  { tag: "@mobile", viewport: { width: 390, height: 844 } },
] as const;

for (const { tag, viewport } of SIZES) {
  test.describe(() => {
    test.use({ viewport });

    // A visually hidden control placed beyond the window once stretched the document, so the window scrolled the shell away (#264).
    test(`the window never scrolls past the shell, even to a hidden control at the end of a long workflow, at ${viewport.width}x${viewport.height} ${tag}`, async ({
      page,
    }) => {
      await page.goto(`/workflows/new?start=${LONG_STARTER}`);
      await expect(page.locator("[data-yaml-editor] .cm-content")).toContainText(`name: ${LONG_STARTER}`);
      await page.getByRole("tab", { name: "Canvas" }).click();
      await expect(page.locator("[data-canvas] .react-flow__node").first()).toBeVisible();
      // Once the checks have answered, whatever tools the stack holds, the page has its full length.
      await expect(page.locator("[data-problems]")).not.toBeEmpty();
      await expect(page.locator("[data-problems]")).not.toContainText("Checking...");

      const main = page.locator("main#main");
      // Again until it holds, since the canvas may still settle and lengthen the page after a scroll.
      await expect(async () => {
        const content = await main.evaluate((element) => {
          element.scrollTo(0, element.scrollHeight);
          return {
            // The content must really run past the window, or there is nothing past the end to escape to.
            overflows: element.scrollHeight > element.clientHeight,
            atEnd: element.scrollTop + element.clientHeight >= element.scrollHeight - 1,
          };
        });
        expect(content).toEqual({ overflows: true, atEnd: true });
      }).toPass();
      // A wheel past the end, in the content's own padding, must not carry on to the window.
      const box = await main.boundingBox();
      if (!box) throw new Error("main has no box");
      await page.mouse.move(box.x + 4, box.y + box.height - 4);
      await page.mouse.wheel(0, viewport.height);
      await page.locator("[data-import]").focus();
      await expect(page.locator("[data-import]")).toBeFocused();

      const windowScroll = await page.evaluate(() => ({ height: document.documentElement.scrollHeight, top: window.scrollY }));
      expect(windowScroll).toEqual({ height: viewport.height, top: 0 });
      await expect(page.locator("[data-top-bar]")).toBeInViewport({ ratio: 1 });
      await expectAccessible(page);
    });
  });
}
