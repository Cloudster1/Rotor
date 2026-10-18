import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { WorkflowResponse, WorkflowSummary } from "@/api/workflows";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";

// The editor's pages load it on demand; loading it once here keeps a busy machine from timing a page out.
beforeAll(async () => {
  await import("./WorkflowRoutes");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const TRIAGE: WorkflowSummary = {
  id: "0190a5c4-0000-7000-8000-00000000aa01",
  name: "triage",
  description: "Decides whether a requirement needs a review.",
  owner: { id: ME.user.id, name: ME.user.name },
  sharing: "groups",
  sharedGroups: ["engineering"],
  currentVersion: 2,
  updatedAt: "2026-10-18T10:00:00Z",
  archived: false,
  canEdit: true,
};

const RESPONSE: WorkflowResponse = {
  workflow: TRIAGE,
  version: {
    number: 2,
    comment: "",
    hash: "h",
    createdAt: "2026-10-18T10:00:00Z",
    createdBy: { id: ME.user.id, name: ME.user.name },
    specYAML: "apiVersion: rotor.dev/v1\nkind: Workflow\n",
  },
};

const VERSIONS = {
  versions: [
    { number: 2, comment: "Ask first", hash: "b", createdAt: "2026-10-18T10:00:00Z", createdBy: { id: ME.user.id, name: ME.user.name } },
    { number: 1, comment: "", hash: "a", createdAt: "2026-10-17T10:00:00Z", createdBy: { id: ME.user.id, name: ME.user.name } },
  ],
};

const DIFF = {
  from: 1,
  to: 2,
  hunks: [
    {
      fromLine: 9,
      toLine: 9,
      lines: [
        { op: "delete", text: "    message: Go?" },
        { op: "insert", text: "    message: Really go?" },
      ],
    },
  ],
};

const workflowAnswers = (workflow: WorkflowResponse = RESPONSE) => ({
  [`/api/v1/workflows/${TRIAGE.id}`]: workflow,
  [`/api/v1/workflows/${TRIAGE.id}/versions`]: VERSIONS,
  [`/api/v1/workflows/${TRIAGE.id}/diff`]: DIFF,
});

describe("the workflows", () => {
  it("are listed with their sharing, and archived after a confirmation", async () => {
    let list = [TRIAGE];
    const { fetch } = await renderAt("/workflows", {
      answers: {
        "/api/v1/workflows": () => Response.json({ workflows: list }),
        [`DELETE /api/v1/workflows/${TRIAGE.id}`]: () => {
          list = [];
          return new Response(null, { status: 204 });
        },
      },
    });
    const link = await screen.findByRole("link", { name: "triage" });
    expect(link).toHaveAttribute("href", `/workflows/${TRIAGE.id}`);
    const row = link.closest("li") as HTMLElement;
    expect(within(row).getByText(s.workflows.sharing.groups)).toBeInTheDocument();
    await userEvent.click(within(row).getByRole("button", { name: s.workflows.archive }));
    await userEvent.click(within(row).getByRole("button", { name: s.workflows.confirm }));
    expect(await screen.findByRole("heading", { level: 2, name: s.workflows.emptyTitle })).toBeInTheDocument();
    const calls = fetch.mock.calls.map(([request]) => `${request.method} ${new URL(request.url).pathname}`);
    expect(calls).toContain(`DELETE /api/v1/workflows/${TRIAGE.id}`);
  });

  it("start from a template, say what is wrong as the server finds it, and save", async () => {
    let posted: { specYAML: string } | undefined;
    await renderAt("/workflows/new", {
      answers: {
        "POST /api/v1/workflows/validate": {
          valid: false,
          problems: [{ path: "/limits", line: 12, column: 1, code: "schema", message: "At /limits: Missing property 'timeout'." }],
        },
        "POST /api/v1/workflows": async (request: Request) => {
          posted = (await request.json()) as { specYAML: string };
          return Response.json({ error: { code: "workflow_invalid", message: "The workflow has 1 problem(s) to fix before it is saved." } }, { status: 422 });
        },
      },
    });
    expect(await screen.findByRole("heading", { level: 1, name: s.workflows.editor.newTitle })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: /Missing property 'timeout'/ })).toHaveTextContent(s.workflows.editor.at(12, 1));
    await userEvent.click(screen.getByRole("button", { name: s.workflows.editor.create }));
    expect(await screen.findByRole("alert")).toHaveTextContent("to fix before it is saved");
    expect(posted?.specYAML).toContain("apiVersion: rotor.dev/v1");
  });

  it("save a new version on top of the one opened", async () => {
    let saved: { baseVersion: number; comment: string } | undefined;
    await renderAt(`/workflows/${TRIAGE.id}`, {
      answers: {
        ...workflowAnswers(),
        [`PUT /api/v1/workflows/${TRIAGE.id}`]: async (request: Request) => {
          saved = (await request.json()) as { baseVersion: number; comment: string };
          return Response.json({ ...RESPONSE, version: { ...RESPONSE.version, number: 3 } });
        },
      },
    });
    expect(await screen.findByRole("heading", { level: 1, name: "triage" })).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText(s.workflows.editor.comment), "Shorter prompt");
    await userEvent.click(screen.getByRole("button", { name: s.workflows.editor.save }));
    await waitFor(() => expect(saved).toEqual(expect.objectContaining({ baseVersion: 2, comment: "Shorter prompt" })));
  });

  it("show what changed between versions, and share with a group", async () => {
    let shared: unknown;
    await renderAt(`/workflows/${TRIAGE.id}`, {
      answers: {
        ...workflowAnswers(),
        [`PUT /api/v1/workflows/${TRIAGE.id}/sharing`]: async (request: Request) => {
          shared = await request.json();
          return Response.json(RESPONSE);
        },
      },
    });
    const versions = await screen.findByRole("region", { name: s.workflows.versions.title });
    await within(versions).findAllByText(s.workflows.version(1));
    await userEvent.click(within(versions).getByRole("button", { name: s.workflows.versions.compare }));
    expect(await within(versions).findByText(/Really go\?/)).toHaveAttribute("data-op", "insert");

    const sharing = screen.getByRole("region", { name: s.workflows.share.title });
    await userEvent.click(within(sharing).getByLabelText("rotor-administrators"));
    await userEvent.click(within(sharing).getByRole("button", { name: s.workflows.share.save }));
    await waitFor(() => expect(shared).toEqual({ sharing: "groups", groups: ["engineering", "rotor-administrators"] }));
  });

  it("are read only for anyone but their owner", async () => {
    await renderAt(`/workflows/${TRIAGE.id}`, {
      answers: workflowAnswers({ ...RESPONSE, workflow: { ...TRIAGE, canEdit: false } }),
    });
    expect(await screen.findByText(s.workflows.editor.readOnly)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.workflows.editor.save })).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: s.workflows.share.title })).not.toBeInTheDocument();
  });

  it("pass axe in the list and the editor", async () => {
    await renderAt("/workflows", { answers: { "/api/v1/workflows": { workflows: [TRIAGE] } } });
    await screen.findByRole("link", { name: "triage" });
    expect(await axeViolations()).toEqual([]);
    cleanup();
    await renderAt(`/workflows/${TRIAGE.id}`, { answers: workflowAnswers() });
    await screen.findByRole("region", { name: s.workflows.versions.title });
    await screen.findByText(s.workflows.editor.valid);
    expect(await axeViolations()).toEqual([]);
  });
});
