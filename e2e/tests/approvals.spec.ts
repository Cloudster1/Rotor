import { expect, type Page, test } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { PEOPLE, signIn } from "../fixtures/auth";
import { createWorkflow, RUN_TIMEOUT_MS, startRun } from "../fixtures/runs";
import { expectAccessible, startInScheme, type ColourScheme } from "../fixtures/shell";

const SCHEMES: ColourScheme[] = ["light", "dark"];

/**
 * A code node drafts a title, an approval lets a person change it, and a second code node answers the title it went
 * on with; no tool, since the tool double answers only whoami. group names who decides, else the starter does.
 */
function reviewed(name: string, group?: string): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    `metadata: {name: ${name}}`,
    "limits: {maxSteps: 10, timeout: 5m}",
    "entry: draft",
    "nodes:",
    "  - id: draft",
    "    type: code",
    "    output: {schema: {type: object, required: [title]}}",
    "    source: |",
    "      def run(state, ctx):",
    '          return {"title": "Drafted title"}',
    "  - id: review",
    "    type: approval",
    '    message: "File {{ nodes.draft.title }}?"',
    group ? `    approvers: {group: ${group}}` : "",
    "    form:",
    "      schema:",
    "        type: object",
    "        required: [title]",
    "        properties:",
    "          title: {type: string, maxLength: 200}",
    "      values:",
    '        title: "{{ nodes.draft.title }}"',
    "  - id: file",
    "    type: code",
    "    output: {schema: {type: object, required: [filed]}}",
    "    source: |",
    "      def run(state, ctx):",
    '          return {"filed": state["nodes"]["review"]["title"]}',
    "edges:",
    "  - {from: draft, to: review}",
    "  - {from: review, to: file}",
    "",
  ]
    .filter((line) => line !== "")
    .join("\n");
}

/** Opens a run's page and waits until the run stops at its approval, answering the approval's address. */
async function waitForApproval(page: Page, runID: string): Promise<string> {
  await page.goto(`/runs/${runID}`);
  await expect(page.locator("[data-run-status]")).toHaveAttribute("data-run-status", "waiting", { timeout: RUN_TIMEOUT_MS });
  const link = page.locator("[data-run-approval] a");
  await expect(link).toBeVisible();
  return (await link.getAttribute("href")) ?? "";
}

for (const scheme of SCHEMES) {
  test(`a member of the named group changes what a run writes and approves it, in ${scheme} @desktop`, async ({ browser }) => {
    const name = `e2e-${randomUUID().slice(0, 12)}`;
    const people = await Promise.all([1, 2].map(() => browser.newContext({ storageState: { cookies: [], origins: [] } })));
    const [bob, carol] = await Promise.all(people.map((context) => context.newPage()));
    try {
      await startInScheme(bob!, scheme);
      await startInScheme(carol!, scheme);
      await signIn(bob!, PEOPLE.bob, "/workflows");
      const workflowID = await createWorkflow(bob!, reviewed(name, "quality"));
      const runID = await startRun(bob!, workflowID);
      const approval = await waitForApproval(bob!, runID);

      // bob started the run, but the step names quality, which carol is in and bob is not.
      await bob!.goto(approval);
      await expect(bob!.locator('[data-action="approve"]')).toHaveCount(0);

      await signIn(carol!, PEOPLE.carol, "/approvals");
      await expect(carol!.locator("[data-approval-badge]")).toBeVisible();
      const card = carol!.locator("li[data-approval]", { hasText: name });
      await expect(card).toBeVisible();
      await expectAccessible(carol!);

      await card.getByRole("link").click();
      await expect(carol!).toHaveURL(new RegExp(`${approval}$`));
      await expect(carol!.locator("[data-approval-message]")).toHaveText("File Drafted title?");
      await expect(carol!.locator('[data-input="title"]')).toHaveValue("Drafted title");
      await expectAccessible(carol!);

      await carol!.locator('[data-input="title"]').fill("Approved title");
      await carol!.locator("[data-approval-comment]").fill("Clearer this way.");
      await carol!.locator('[data-action="approve"]').click();
      await expect(carol!.locator("[data-approval-status]")).toHaveAttribute("data-approval-status", "approved");
      await expect(carol!.locator("[data-approval-decided]")).toContainText("Clearer this way.");

      await bob!.goto(`/runs/${runID}`);
      await expect(bob!.locator("[data-run-status]")).toHaveAttribute("data-run-status", "succeeded", { timeout: RUN_TIMEOUT_MS });
      await expect(bob!.locator("[data-run-output]")).toContainText("Approved title");
      await expectAccessible(bob!);
    } finally {
      await Promise.all(people.map((context) => context.close()));
    }
  });
}

test("the person who started a run rejects its approval, and the run ends cancelled", async ({ page }) => {
  const name = `e2e-${randomUUID().slice(0, 12)}`;
  const workflowID = await createWorkflow(page, reviewed(name));
  const runID = await startRun(page, workflowID);
  const approval = await waitForApproval(page, runID);

  await page.goto(approval);
  await page.locator("[data-approval-comment]").fill("Not now.");
  await page.locator('[data-action="reject"]').click();
  await expectAccessible(page);
  await page.locator('[data-action="confirm-reject"]').click();
  await expect(page.locator("[data-approval-status]")).toHaveAttribute("data-approval-status", "rejected");

  await page.goto(`/runs/${runID}`);
  await expect(page.locator("[data-run-status]")).toHaveAttribute("data-run-status", "cancelled", { timeout: RUN_TIMEOUT_MS });
  await expect(page.locator("[data-run-timeline]")).toContainText("Not now.");
});
