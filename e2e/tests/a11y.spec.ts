import { expect, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { archiveWorkflow, plainWorkflow } from "../fixtures/access";
import { ALICE_STATE } from "../fixtures/auth";
import { createWorkflow } from "../fixtures/runs";
import { ADMIN_PAGES, expectAccessible, openShell, PLACES, SETTINGS_PAGES, startInScheme, type ColourScheme } from "../fixtures/shell";

// Contrast needs real layout and real colours, which is why these run here and not in the jsdom suite.
// The workflow editor is no sidebar place, but its canvas and inspector are pages of their own to check (#46),
// and so is every page of Settings and Admin (#199), with each tab of Access (#203) and Single sign-on (#204), and
// each section of a workflow's settings (#205).
const ACCESS_TABS = ["/admin/access?tab=matrix", "/admin/access?tab=groups", "/admin/access?tab=sso"];
const PATHS = [
  ...new Set([
    ...PLACES.map((place) => place.opens),
    ...[...SETTINGS_PAGES, ...ADMIN_PAGES].map((page) => page.path),
    ...ACCESS_TABS,
    "/workflows/new",
    "/no-such-page",
  ]),
];
const SCHEMES: ColourScheme[] = ["light", "dark"];

for (const scheme of SCHEMES) {
  for (const path of PATHS) {
    test(`${path} passes axe in ${scheme}`, async ({ page }) => {
      await startInScheme(page, scheme);
      await openShell(page, path);
      await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
      await expect(page.locator("main h1")).toBeVisible();
      await expectAccessible(page);
    });
  }
}

const WORKFLOW_SECTIONS = ["general", "versions", "sharing", "triggers"] as const;

test.describe("a workflow's settings", () => {
  let owner: Page;
  let workflowID = "";
  test.beforeAll(async ({ browser }) => {
    owner = await (await browser.newContext({ storageState: ALICE_STATE })).newPage();
    workflowID = await createWorkflow(owner, plainWorkflow(`e2e-a11y-${randomUUID().slice(0, 8)}`));
  });
  test.afterAll(async () => {
    await archiveWorkflow(owner, workflowID);
    await owner.context().close();
  });

  for (const scheme of SCHEMES) {
    for (const section of WORKFLOW_SECTIONS) {
      test(`/workflows/<id>/settings/${section} passes axe in ${scheme}`, async ({ page }) => {
        await startInScheme(page, scheme);
        await openShell(page, `/workflows/${workflowID}/settings/${section}`);
        await expect(page.locator("html")).toHaveAttribute("data-theme", scheme);
        await expect(page.locator(`[data-settings-page="${section}"]`)).toHaveAttribute("aria-current", "page");
        await expect(page.locator("main h1")).toBeVisible();
        await expectAccessible(page);
      });
    }
  }
});
