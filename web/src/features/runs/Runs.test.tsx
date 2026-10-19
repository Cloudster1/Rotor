import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Run } from "@/api/runs";
import type { WorkflowResponse, WorkflowSummary } from "@/api/workflows";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";

// A run's page loads on demand with React Flow; loading it once here keeps a busy machine from timing a page out.
beforeAll(async () => {
  await import("./RunRoute");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const DRAFT: WorkflowSummary = {
  id: "0190a5c4-0000-7000-8000-00000000bb01",
  name: "draft-requirements",
  description: "",
  owner: { id: ME.user.id, name: ME.user.name },
  sharing: "private",
  sharedGroups: [],
  currentVersion: 1,
  updatedAt: "2026-10-18T10:00:00Z",
  archived: false,
  canEdit: true,
};

const RESPONSE: WorkflowResponse = {
  workflow: DRAFT,
  version: {
    number: 1,
    comment: "",
    hash: "h",
    createdAt: "2026-10-18T10:00:00Z",
    createdBy: { id: ME.user.id, name: ME.user.name },
    specYAML: "apiVersion: rotor.dev/v1\n",
    inputs: {
      type: "object",
      required: ["topic"],
      properties: {
        topic: { type: "string", description: "What the requirement is about." },
        count: { type: "integer" },
        urgent: { type: "boolean" },
        tone: { type: "string", enum: ["plain", "formal"] },
        extra: { type: "object" },
      },
    },
    bindings: [{ name: "exciter", tool: "exciter", scopes: ["mcp"], access: "write" }],
  },
};

const RUN: Run = {
  id: "0190a5c4-0000-7000-8000-00000000cc01",
  workflow: { id: DRAFT.id, name: DRAFT.name },
  version: 1,
  status: "running",
  startedBy: { id: ME.user.id, name: ME.user.name },
  reason: "",
  inputs: { topic: "billing" },
  output: null,
  createdAt: "2026-10-18T10:00:00Z",
  updatedAt: "2026-10-18T10:00:00Z",
  startedAt: "2026-10-18T10:00:01Z",
  finishedAt: null,
  ended: false,
  approvalId: null,
  trigger: null,
  usage: { inputTokens: 0, outputTokens: 0, cost: 0 },
};

const listed = { "/api/v1/workflows": { workflows: [DRAFT] }, [`/api/v1/workflows/${DRAFT.id}`]: RESPONSE };

async function openDialog() {
  await userEvent.click(await screen.findByRole("button", { name: s.runs.runNamed(DRAFT.name) }));
  return screen.findByRole("dialog", { name: s.runs.dialog.title(DRAFT.name) });
}

describe("starting a run", () => {
  it("asks for the workflow's inputs, says what the server refuses, and starts the run with one key", async () => {
    const keys: string[] = [];
    const bodies: unknown[] = [];
    let attempt = 0;
    await renderAt("/workflows", {
      answers: {
        ...listed,
        [`POST /api/v1/workflows/${DRAFT.id}/runs`]: async (request: Request) => {
          keys.push(request.headers.get("Idempotency-Key") ?? "");
          bodies.push(await request.json());
          attempt += 1;
          if (attempt === 1) {
            return Response.json(
              { error: { code: "inputs_invalid", message: "The inputs do not fit.", fields: { "inputs/topic": "At /topic: required." } } },
              { status: 422 },
            );
          }
          return Response.json({ ...RUN, status: "queued" }, { status: 201 });
        },
        [`/api/v1/runs/${RUN.id}`]: RUN,
      },
    });
    const dialog = await openDialog();
    expect(within(dialog).getByText(s.runs.dialog.asYou)).toBeInTheDocument();
    expect(within(dialog).getByText(/exciter \(reads and writes\)/)).toBeInTheDocument();
    expect(within(dialog).getByLabelText(/count/)).toHaveAttribute("type", "number");
    expect(within(dialog).getByLabelText(/urgent/).tagName).toBe("SELECT");
    expect(within(dialog).getByLabelText(/extra/).tagName).toBe("TEXTAREA");

    await userEvent.click(within(dialog).getByRole("button", { name: s.runs.dialog.start }));
    expect(await within(dialog).findByText("At /topic: required.")).toBeInTheDocument();
    expect(within(dialog).getByLabelText(/topic/)).toHaveAttribute("aria-invalid", "true");

    await userEvent.type(within(dialog).getByLabelText(/topic/), "billing");
    await userEvent.type(within(dialog).getByLabelText(/count/), "3");
    await userEvent.selectOptions(within(dialog).getByLabelText(/urgent/), "true");
    await userEvent.click(within(dialog).getByRole("button", { name: s.runs.dialog.start }));
    expect(await screen.findByRole("heading", { level: 1, name: s.runs.page.title(DRAFT.name) })).toBeInTheDocument();
    expect(bodies[1]).toEqual({ inputs: { topic: "billing", count: 3, urgent: true } });
    expect(keys[0]).not.toBe("");
    expect(keys[1]).toBe(keys[0]);
  });

  it("refuses a field that is not JSON before asking the server", async () => {
    const { fetch } = await renderAt("/workflows", { answers: listed });
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText(/topic/), "x");
    await userEvent.type(within(dialog).getByLabelText(/extra/), "not json");
    await userEvent.click(within(dialog).getByRole("button", { name: s.runs.dialog.start }));
    expect(await within(dialog).findByText(s.runs.dialog.notJSON("extra"))).toBeInTheDocument();
    expect(fetch.mock.calls.some(([request]) => (request as Request).method === "POST")).toBe(false);
  });

  it("offers connecting the tools the run acts in, and comes back to the workflow", async () => {
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign });
    let next = "";
    await renderAt("/workflows", {
      answers: {
        ...listed,
        [`POST /api/v1/workflows/${DRAFT.id}/runs`]: () =>
          Response.json(
            { error: { code: "connections_needed", message: "Connect exciter first.", fields: { exciter: "tool-1", stator: "" } } },
            { status: 409 },
          ),
        "POST /api/v1/tools/tool-1/connection": (request: Request) => {
          next = new URL(request.url).searchParams.get("next") ?? "";
          return Response.json({ authorizeURL: "https://exciter.example/authorize" });
        },
      },
    });
    const dialog = await openDialog();
    await userEvent.type(within(dialog).getByLabelText(/topic/), "billing");
    await userEvent.click(within(dialog).getByRole("button", { name: s.runs.dialog.start }));
    const notice = await within(dialog).findByText(s.runs.dialog.connectFirst);
    expect(within(notice.parentElement as HTMLElement).getByText(s.runs.dialog.missingTool("stator"))).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: s.connections.connect("exciter") }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith("https://exciter.example/authorize"));
    expect(next).toBe(`/workflows/${DRAFT.id}`);
  });

  it("passes axe with the dialog open", async () => {
    await renderAt("/workflows", { answers: listed });
    await openDialog();
    expect(await axeViolations()).toEqual([]);
  });
});

describe("a run's page", () => {
  it("says where the run is and cancels it after a confirmation", async () => {
    let current: Run = RUN;
    await renderAt(`/runs/${RUN.id}`, {
      answers: {
        [`/api/v1/runs/${RUN.id}`]: () => Response.json(current),
        [`POST /api/v1/runs/${RUN.id}/cancel`]: () => {
          current = { ...RUN, status: "cancelled", reason: "cancelled", ended: true, finishedAt: "2026-10-18T10:01:00Z" };
          return Response.json(current);
        },
      },
    });
    expect(await screen.findByText(s.runs.status.running)).toBeInTheDocument();
    expect(screen.getByText(s.runs.page.startedBy(ME.user.name))).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: s.runs.page.cancel }));
    await userEvent.click(screen.getByRole("button", { name: s.runs.page.confirm }));
    expect(await screen.findByText(s.runs.status.cancelled)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.runs.page.cancel })).not.toBeInTheDocument();
  });

  it("shows what a finished run answered", async () => {
    await renderAt(`/runs/${RUN.id}`, {
      answers: { [`/api/v1/runs/${RUN.id}`]: { ...RUN, status: "succeeded", ended: true, output: { greet: { text: "Hello, bob" } } } },
    });
    expect(await screen.findByText(/Hello, bob/)).toBeInTheDocument();
    expect(screen.queryByText(s.runs.page.live)).not.toBeInTheDocument();
  });

  it("says so when the run is not there", async () => {
    await renderAt(`/runs/${RUN.id}`);
    expect(await screen.findByText(s.runs.page.notFound)).toBeInTheDocument();
  });
});
