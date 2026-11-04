import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Assignment } from "@/api/access";
import type { WorkflowResponse } from "@/api/workflows";
import { s } from "@/strings";
import { ACCESS_ANSWERS, BOB, ENGINEERING, grant } from "@/test/access";
import { ME, renderAt } from "@/test/app";
import { axeViolations } from "@/test/axe";

beforeAll(async () => {
  await import("./settings/WorkflowSettings");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const ID = "0190a5c4-0000-7000-8000-00000000aa01";
const WORKFLOW: WorkflowResponse = {
  workflow: {
    id: ID,
    name: "triage",
    description: "",
    owner: { id: ME.user.id, name: ME.user.name },
    sharedWith: 0,
    currentVersion: 1,
    updatedAt: "2026-10-18T10:00:00Z",
    archived: false,
    canRun: true,
    canEdit: true,
    canAdminister: true,
  },
  version: {
    number: 1,
    comment: "",
    hash: "h",
    createdAt: "2026-10-18T10:00:00Z",
    createdBy: { id: ME.user.id, name: ME.user.name },
    specYAML: "apiVersion: rotor.dev/v1\nkind: Workflow\n",
    inputs: null,
    bindings: [],
    unattendedCalls: [],
  },
};
const BOB_RUNS = grant("g1", { userId: BOB.id, userName: BOB.name, userEmail: BOB.email, workflowId: ID, workflowName: "triage" });
const ENGINEERING_EDITS = grant("g2", { role: "editor", roleName: "Editor", groupId: ENGINEERING.id, groupName: "engineering", workflowId: ID });

function serve(workflow = WORKFLOW) {
  let grants: Assignment[] = [BOB_RUNS];
  const sent: string[] = [];
  const answers = {
    ...ACCESS_ANSWERS,
    [`/api/v1/workflows/${ID}`]: workflow,
    [`/api/v1/workflows/${ID}/versions`]: { versions: [] },
    [`/api/v1/workflows/${ID}/grants`]: () => Response.json({ grants }),
    [`POST /api/v1/workflows/${ID}/grants`]: async (request: Request) => {
      sent.push(`POST ${JSON.stringify(await request.json())}`);
      grants = [...grants, ENGINEERING_EDITS];
      return Response.json({ grant: ENGINEERING_EDITS }, { status: 201 });
    },
    [`DELETE /api/v1/workflows/${ID}/grants/${BOB_RUNS.id}`]: () => {
      sent.push(`DELETE ${BOB_RUNS.id}`);
      grants = grants.filter((each) => each.id !== BOB_RUNS.id);
      return new Response(null, { status: 204 });
    },
  };
  return { answers, sent };
}

const holders = () => [...document.querySelectorAll("[data-grant]")].map((each) => `${each.getAttribute("data-grantee")}: ${each.getAttribute("data-role")}`);

describe("a workflow's sharing", () => {
  it("lists who holds a role on it, read only, for somebody who does not administer it", async () => {
    const reader = { ...WORKFLOW, workflow: { ...WORKFLOW.workflow, canEdit: false, canAdminister: false } };
    const { fetch } = await renderAt(`/workflows/${ID}/settings/sharing`, { me: { ...ME, role: "member" }, answers: serve(reader).answers });
    const panel = within(await screen.findByRole("region", { name: s.workflowGrants.title }));
    await waitFor(() => expect(holders()).toEqual([`${BOB.name}: runner`]));
    expect(panel.getByText("Runner")).toBeInTheDocument();
    expect(panel.getByText(s.workflowGrants.readOnly)).toBeInTheDocument();
    expect(panel.queryByRole("button")).toBeNull();
    expect(panel.queryByRole("form")).toBeNull();
    // Who may be picked is the administrators' to ask; a reader never asks for it.
    const asked = fetch.mock.calls.map(([request]) => new URL(request.url).pathname);
    expect(asked).not.toContain("/api/v1/groups");
    expect(asked).not.toContain("/api/v1/people");
    expect(await axeViolations()).toEqual([]);
  });

  it("lets its administrator share it with a group as an editor and take a grant away", async () => {
    const server = serve();
    const { fetch } = await renderAt(`/workflows/${ID}/settings/sharing`, { answers: server.answers });
    const region = await screen.findByRole("region", { name: s.workflowGrants.title });
    const panel = within(region);
    await waitFor(() => expect(holders()).toEqual([`${BOB.name}: runner`]));
    const form = within(panel.getByRole("form", { name: s.workflowGrants.share }));
    const role = form.getByLabelText(s.workflowGrants.role);
    expect(
      within(role)
        .getAllByRole("option")
        .map((option) => option.textContent),
    ).toEqual(["Runner", "Editor", "Workflow administrator"]);
    const who = form.getByLabelText(s.workflowGrants.who);
    await within(who).findByRole("option", { name: "engineering" });
    expect(within(who).getAllByRole("option")[1]).toHaveTextContent(s.access.groups.everyone);
    await userEvent.selectOptions(who, "engineering");
    await userEvent.selectOptions(role, "Editor");
    await userEvent.click(form.getByRole("button", { name: s.workflowGrants.share }));
    await waitFor(() => expect(server.sent).toEqual([`POST ${JSON.stringify({ role: "editor", groupId: ENGINEERING.id })}`]));
    await waitFor(() => expect(holders()).toEqual([`${BOB.name}: runner`, "engineering: editor"]));
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(panel.getByRole("button", { name: s.workflowGrants.removeFrom("Runner", BOB.name) }));
    const dialog = screen.getByRole("dialog", { name: s.workflowGrants.removeTitle(BOB.name) });
    await userEvent.click(within(dialog).getByRole("button", { name: s.workflowGrants.removeGrant }));
    await waitFor(() => expect(holders()).toEqual(["engineering: editor"]));
    expect(server.sent.at(-1)).toBe(`DELETE ${BOB_RUNS.id}`);
    // Sharing is the grants now; the old sharing endpoint is never called.
    expect(fetch.mock.calls.map(([request]) => new URL(request.url).pathname)).not.toContain(`/api/v1/workflows/${ID}/sharing`);
  });
});
