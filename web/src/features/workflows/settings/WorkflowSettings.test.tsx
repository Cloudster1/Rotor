import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { WorkflowResponse, WorkflowSummary } from "@/api/workflows";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";

beforeAll(async () => {
  await import("./WorkflowSettings");
  await import("../WorkflowRoutes");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const ID = "0190a5c4-0000-7000-8000-00000000ee01";
const SETTINGS = `/workflows/${ID}/settings`;

const YAML = `apiVersion: rotor.dev/v1
kind: Workflow
metadata:
  name: triage # what the list shows
  description: Decides whether a requirement needs a review.
entry: ask
nodes:
  - {id: ask, type: approval, message: "Go on?"}
`;

const WORKFLOW: WorkflowSummary = {
  id: ID,
  name: "triage",
  description: "Decides whether a requirement needs a review.",
  owner: { id: ME.user.id, name: ME.user.name },
  sharedWith: 1,
  currentVersion: 2,
  updatedAt: "2026-10-18T10:00:00Z",
  archived: false,
  canRun: true,
  canEdit: true,
  canAdminister: true,
};

const version = (number: number, specYAML = YAML) => ({
  number,
  comment: "",
  hash: `h${number}`,
  createdAt: "2026-10-18T10:00:00Z",
  createdBy: { id: ME.user.id, name: ME.user.name },
  specYAML,
  inputs: null,
  bindings: [],
  unattendedCalls: [],
});

const summaryOf = (v: ReturnType<typeof version>) => ({ number: v.number, comment: v.comment, hash: v.hash, createdAt: v.createdAt, createdBy: v.createdBy });

const response = (workflow: Partial<WorkflowSummary> = {}): WorkflowResponse => ({ workflow: { ...WORKFLOW, ...workflow }, version: version(2) });

const RUNNER = { canEdit: false, canAdminister: false };
const READER = { canRun: false, canEdit: false, canAdminister: false };

const answers = (workflow: WorkflowResponse | (() => Response) = response()) => ({
  [`/api/v1/workflows/${ID}`]: workflow,
  [`/api/v1/workflows/${ID}/versions`]: { versions: [2, 1].map((number) => summaryOf(version(number))) },
  [`/api/v1/workflows/${ID}/versions/1`]: version(1, YAML.replace("Go on?", "Go?")),
});

const pathname = (router: { state: { location: { pathname: string } } }) => router.state.location.pathname;

describe("a workflow's settings", () => {
  it("open General from /settings, with a side nav of its five sections and a way back to the editor", async () => {
    const { router } = await renderAt(SETTINGS, { answers: answers() });
    expect(await screen.findByRole("heading", { level: 1, name: s.workflowSettings.pages.general })).toBeInTheDocument();
    expect(pathname(router)).toBe(`${SETTINGS}/general`);
    const nav = screen.getByRole("navigation", { name: s.workflowSettings.nav });
    expect(
      within(nav)
        .getAllByRole("link")
        .map((link) => link.getAttribute("href")),
    ).toEqual(["general", "versions", "sharing", "triggers", "tracing"].map((section) => `${SETTINGS}/${section}`));
    expect(screen.getByText(s.workflowSettings.of("triage"))).toBeInTheDocument();
    expect(screen.getByRole("link", { name: s.workflowSettings.editor })).toHaveAttribute("href", `/workflows/${ID}`);
    await userEvent.click(within(nav).getByRole("link", { name: s.workflowSettings.pages.triggers }));
    expect(await screen.findByRole("heading", { level: 1, name: s.workflowSettings.pages.triggers })).toBeInTheDocument();
    expect(pathname(router)).toBe(`${SETTINGS}/triggers`);
  });

  it.each([
    ["general", s.workflowSettings.pages.general],
    ["versions", s.workflowSettings.pages.versions],
    ["sharing", s.workflowSettings.pages.sharing],
    ["triggers", s.workflowSettings.pages.triggers],
    ["tracing", s.workflowSettings.pages.tracing],
  ])("give %s a route of its own that passes axe", async (section, title) => {
    await renderAt(`${SETTINGS}/${section}`, { answers: answers() });
    expect(await screen.findByRole("heading", { level: 1, name: title })).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("say so when the workflow is not there or not shared with you", async () => {
    await renderAt(`${SETTINGS}/general`);
    expect(await screen.findByRole("alert")).toHaveTextContent(s.workflowSettings.notFound);
  });

  it("save a new name and description as a version whose YAML keeps everything else", async () => {
    let saved: { specYAML: string; comment: string; baseVersion: number } | undefined;
    await renderAt(`${SETTINGS}/general`, {
      answers: {
        ...answers(),
        [`PUT /api/v1/workflows/${ID}`]: async (request: Request) => {
          saved = (await request.json()) as typeof saved;
          return Response.json({ ...response({ name: "triage-v2" }), version: version(3) });
        },
      },
    });
    const name = await screen.findByLabelText(s.workflowSettings.general.name);
    await userEvent.clear(name);
    await userEvent.type(name, "triage-v2");
    await userEvent.clear(screen.getByLabelText(s.workflowSettings.general.description));
    await userEvent.click(screen.getByRole("button", { name: s.workflowSettings.general.save }));
    expect(await screen.findByText(s.workflowSettings.general.saved(3))).toBeInTheDocument();
    expect(saved).toEqual({ specYAML: expect.any(String), comment: s.workflowSettings.general.comment, baseVersion: 2 });
    expect(saved?.specYAML).toContain("  name: triage-v2 # what the list shows\n");
    expect(saved?.specYAML).not.toContain("description:");
    expect(saved?.specYAML).toContain('message: "Go on?"');
  });

  it("refuse an empty name before asking the server", async () => {
    const { fetch } = await renderAt(`${SETTINGS}/general`, { answers: answers() });
    await userEvent.clear(await screen.findByLabelText(s.workflowSettings.general.name));
    await userEvent.click(screen.getByRole("button", { name: s.workflowSettings.general.save }));
    expect(await screen.findByRole("alert")).toHaveTextContent(s.workflowSettings.general.nameNeeded);
    expect(fetch.mock.calls.some(([request]) => request.method === "PUT")).toBe(false);
  });

  it("archive at once and offer Undo, which restores it", async () => {
    let archived = false;
    const { fetch } = await renderAt(`${SETTINGS}/general`, {
      answers: {
        ...answers(() => Response.json(response({ archived }))),
        [`DELETE /api/v1/workflows/${ID}`]: () => {
          archived = true;
          return new Response(null, { status: 204 });
        },
        [`POST /api/v1/workflows/${ID}/restore`]: () => {
          archived = false;
          return Response.json(response());
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: s.workflowSettings.general.archive }));
    const status = screen.getByRole("status");
    expect(await within(status).findByText(s.workflowSettings.general.archived("triage"))).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.workflowSettings.general.restore })).not.toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(status).getByRole("button", { name: s.workflowSettings.general.undo }));
    expect(await within(status).findByText(s.workflowSettings.general.restored("triage"))).toBeInTheDocument();
    expect(screen.getByRole("button", { name: s.workflowSettings.general.archive })).toBeInTheDocument();
    const calls = fetch.mock.calls.map(([request]) => `${request.method} ${new URL(request.url).pathname}`);
    expect(calls).toEqual(expect.arrayContaining([`DELETE /api/v1/workflows/${ID}`, `POST /api/v1/workflows/${ID}/restore`]));
  });

  it("offer an archived workflow's administrator to restore it, and its runner only to read it", async () => {
    await renderAt(`${SETTINGS}/general`, { answers: answers(response({ archived: true })) });
    expect(await screen.findByText(s.workflowSettings.general.isArchived)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: s.workflowSettings.general.restore })).toBeInTheDocument();
    expect(screen.getByText(s.workflowSettings.general.archivedReadOnly)).toBeInTheDocument();
    cleanup();
    await renderAt(`${SETTINGS}/general`, { answers: answers(response({ archived: true, ...RUNNER })) });
    expect(await screen.findByText(s.workflowSettings.general.archiveReadOnly)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.workflowSettings.general.restore })).not.toBeInTheDocument();
  });

  it("show General read only to a runner: no saving, no archiving", async () => {
    await renderAt(`${SETTINGS}/general`, { me: { ...ME, role: "member" }, answers: answers(response(RUNNER)) });
    expect(await screen.findByLabelText(s.workflowSettings.general.name)).toHaveAttribute("readonly");
    expect(screen.getByLabelText(s.workflowSettings.general.description)).toHaveAttribute("readonly");
    expect(screen.getByText(s.workflowSettings.general.readOnly)).toBeInTheDocument();
    expect(screen.getByText(s.workflowSettings.general.archiveReadOnly)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.workflowSettings.general.save })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.workflowSettings.general.archive })).not.toBeInTheDocument();
  });

  it("let an editor rename the workflow but not archive it", async () => {
    await renderAt(`${SETTINGS}/general`, { answers: answers(response({ canAdminister: false })) });
    expect(await screen.findByRole("button", { name: s.workflowSettings.general.save })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.workflowSettings.general.archive })).not.toBeInTheDocument();
  });

  it("show Versions to a runner, and open an older one in the editor by its number", async () => {
    const { router } = await renderAt(`${SETTINGS}/versions`, { me: { ...ME, role: "member" }, answers: answers(response(RUNNER)) });
    const versions = await screen.findByRole("region", { name: s.workflows.versions.title });
    await userEvent.click(await within(versions).findByRole("button", { name: s.workflows.versions.compare }));
    await userEvent.click(within(versions).getByRole("button", { name: `${s.workflows.versions.open} ${s.workflows.version(1)}` }));
    expect(await screen.findByText(s.workflowSettings.openedVersion(1))).toBeInTheDocument();
    expect(router.state.location.href).toBe(`/workflows/${ID}?version=1`);
  });

  it("show Sharing read only to a runner", async () => {
    await renderAt(`${SETTINGS}/sharing`, { me: { ...ME, role: "member" }, answers: answers(response(RUNNER)) });
    const panel = await screen.findByRole("region", { name: s.workflowGrants.title });
    expect(within(panel).getByText(s.workflowGrants.readOnly)).toBeInTheDocument();
    expect(within(panel).queryByRole("form")).toBeNull();
  });

  it("let a runner add triggers of their own, and show them read only to somebody who may only read", async () => {
    await renderAt(`${SETTINGS}/triggers`, { me: { ...ME, role: "member" }, answers: answers(response(RUNNER)) });
    expect(await screen.findByRole("button", { name: s.triggers.add })).toBeInTheDocument();
    expect(screen.queryByText(s.workflowSettings.triggersReadOnly)).not.toBeInTheDocument();
    cleanup();
    await renderAt(`${SETTINGS}/triggers`, { me: { ...ME, role: "member" }, answers: answers(response(READER)) });
    expect(await screen.findByText(s.workflowSettings.triggersReadOnly)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.triggers.add })).not.toBeInTheDocument();
    cleanup();
    await renderAt(`${SETTINGS}/triggers`, { answers: answers(response({ archived: true })) });
    expect(await screen.findByText(s.workflowSettings.triggersArchived)).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("button", { name: s.triggers.add })).not.toBeInTheDocument());
  });
});
