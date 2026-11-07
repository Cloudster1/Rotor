import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { WorkflowResponse, WorkflowSummary } from "@/api/workflows";
import type { WorkflowTracingRequest, WorkflowTracingResponse } from "@/api/workflowTracing";
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

const ID = "0190a5c4-0000-7000-8000-00000000ee02";
const PAGE = `/workflows/${ID}/settings/tracing`;
const t = s.workflowSettings.tracing;

const WORKFLOW: WorkflowSummary = {
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
};

const workflow: WorkflowResponse = {
  workflow: WORKFLOW,
  version: {
    number: 1,
    comment: "",
    hash: "h1",
    createdAt: "2026-10-18T10:00:00Z",
    createdBy: { id: ME.user.id, name: ME.user.name },
    specYAML: "apiVersion: rotor.dev/v1\nkind: Workflow\nmetadata:\n  name: triage\nentry: ask\nnodes:\n  - {id: ask, type: approval, message: Go on?}\n",
    inputs: null,
    bindings: [],
    unattendedCalls: [],
  },
};

const TRACING = { content: "inherit", destinations: null, sampleRatio: 1, changedBy: null, updatedAt: null } as const;

const tracingOf = (canChange: boolean, tracing: Partial<WorkflowTracingResponse["tracing"]> = {}): WorkflowTracingResponse => ({
  tracing: { ...TRACING, ...tracing },
  installation: { keepContent: true },
  destinations: [
    { id: "d1", name: "langfuse", enabled: true, content: true },
    { id: "d2", name: "tempo", enabled: true, content: false },
    { id: "d3", name: "old-collector", enabled: false, content: false },
  ],
  canChange,
});

const answers = (tracing: WorkflowTracingResponse, put?: (request: Request) => Promise<Response>) => ({
  [`/api/v1/workflows/${ID}`]: workflow,
  [`/api/v1/workflows/${ID}/settings/tracing`]: tracing,
  ...(put ? { [`PUT /api/v1/workflows/${ID}/settings/tracing`]: put } : {}),
});

describe("a workflow's tracing page", () => {
  it("shows what the installation does and passes axe", async () => {
    await renderAt(PAGE, { answers: answers(tracingOf(true)) });
    expect(await screen.findByText(t.installationKeeps)).toBeInTheDocument();
    expect(screen.getByText(t.destinationsWithText(["langfuse"]))).toBeInTheDocument();
    expect(screen.getByLabelText(new RegExp(t.inherit))).toBeChecked();
    expect(screen.getByLabelText(new RegExp(t.everyDestination))).toBeChecked();
    expect(screen.getByLabelText(t.shareLabel)).toHaveValue(1);
    expect(await axeViolations()).toEqual([]);
  });

  it("save no text, only the chosen destinations and a share of the runs", async () => {
    let body: WorkflowTracingRequest | undefined;
    await renderAt(PAGE, {
      answers: answers(tracingOf(true), async (request) => {
        body = (await request.json()) as WorkflowTracingRequest;
        return Response.json({ ...tracingOf(true, body), destinations: tracingOf(true).destinations });
      }),
    });
    await userEvent.click(await screen.findByLabelText(new RegExp(t.none)));
    await userEvent.click(screen.getByLabelText(new RegExp(t.chosenDestinations)));
    const chosen = screen.getByRole("group", { name: t.chosenLegend });
    expect(within(chosen).getByLabelText(/old-collector \(disabled\)/)).toBeInTheDocument();
    await userEvent.click(within(chosen).getByLabelText(/^langfuse/));
    const share = screen.getByLabelText(t.shareLabel);
    await userEvent.clear(share);
    await userEvent.type(share, "0.25");
    await userEvent.click(screen.getByRole("button", { name: t.save }));
    expect(await screen.findByText(t.saved)).toBeInTheDocument();
    expect(body).toEqual({ content: "none", destinations: ["d1"], sampleRatio: 0.25 });
  });

  it("show it read only to somebody who does not administer the workflow", async () => {
    await renderAt(PAGE, { answers: answers(tracingOf(false, { content: "none", changedBy: "Ada" })) });
    expect(await screen.findByText(t.readOnly)).toHaveAttribute("data-read-only", "tracing");
    expect(screen.getByLabelText(new RegExp(t.none))).toBeDisabled();
    expect(screen.getByLabelText(t.shareLabel)).toBeDisabled();
    expect(screen.getByText(t.changedBy("Ada"))).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: t.save })).not.toBeInTheDocument();
  });

  it("refuse a share outside 0 to 1 with a sentence before asking the server", async () => {
    const { fetch } = await renderAt(PAGE, { answers: answers(tracingOf(true)) });
    const share = await screen.findByLabelText(t.shareLabel);
    await userEvent.clear(share);
    await userEvent.type(share, "1.5");
    await userEvent.click(screen.getByRole("button", { name: t.save }));
    expect(await screen.findByRole("alert")).toHaveTextContent(t.shareProblem);
    expect(fetch.mock.calls.some(([request]) => request.method === "PUT")).toBe(false);
  });
});
