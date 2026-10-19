import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Trigger } from "@/api/triggers";
import type { WorkflowResponse, WorkflowSummary } from "@/api/workflows";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";

beforeAll(async () => {
  await import("@/features/workflows/WorkflowRoutes");
});

// The time-zone list holds every zone the browser knows, which makes axe slow over the form.
const FORM_TEST_MS = 20_000;

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const WORKFLOW: WorkflowSummary = {
  id: "0190a5c4-0000-7000-8000-00000000bb01",
  name: "weekly-report",
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
  workflow: WORKFLOW,
  version: {
    number: 1,
    comment: "",
    hash: "h",
    createdAt: "2026-10-18T10:00:00Z",
    createdBy: { id: ME.user.id, name: ME.user.name },
    specYAML: "apiVersion: rotor.dev/v1\nkind: Workflow\n",
    inputs: { type: "object", properties: { project: { type: "string" } } },
    bindings: [],
  },
};

const TRIGGER: Trigger = {
  id: "0190a5c4-0000-7000-8000-00000000cc01",
  workflow: { id: WORKFLOW.id, name: WORKFLOW.name },
  owner: { id: ME.user.id, name: ME.user.name },
  name: "Mondays",
  kind: "schedule",
  enabled: false,
  disabled: { reason: "connection_lost", message: "The workflow uses a tool you are no longer connected to.", at: "2026-10-19T08:00:00Z" },
  schedule: { cron: "0 8 * * 1", timeZone: "Europe/Berlin", noOverlap: true },
  event: null,
  inputs: { project: "RTR" },
  nextRunAt: null,
  lastFire: { outcome: "started", reason: "", at: "2026-10-12T06:00:00Z", runId: "0190a5c4-0000-7000-8000-00000000dd01", runStatus: "succeeded" },
  createdAt: "2026-10-01T10:00:00Z",
  updatedAt: "2026-10-19T08:00:00Z",
};

const pageAnswers = (extra: Record<string, unknown> = {}) => ({
  [`/api/v1/workflows/${WORKFLOW.id}`]: RESPONSE,
  [`/api/v1/workflows/${WORKFLOW.id}/versions`]: { versions: [] },
  ...extra,
});

describe("a workflow's triggers", () => {
  it(
    "are added with a schedule, its next runs and the inputs each run starts with",
    async () => {
      let posted: Record<string, unknown> | undefined;
      let list: Trigger[] = [];
      await renderAt(`/workflows/${WORKFLOW.id}`, {
        answers: pageAnswers({
          [`/api/v1/workflows/${WORKFLOW.id}/triggers`]: () => Response.json({ triggers: list }),
          "POST /api/v1/triggers/schedule": { nextRuns: ["2026-10-26T07:00:00Z", "2026-11-02T07:00:00Z"] },
          [`POST /api/v1/workflows/${WORKFLOW.id}/triggers`]: async (request: Request) => {
            posted = (await request.json()) as Record<string, unknown>;
            list = [{ ...TRIGGER, enabled: true, disabled: null, nextRunAt: "2026-10-26T07:00:00Z", lastFire: null }];
            return Response.json(list[0], { status: 201 });
          },
        }),
      });
      const section = await screen.findByRole("region", { name: s.triggers.title });
      expect(within(section).getByText(s.triggers.none)).toBeInTheDocument();
      await userEvent.click(within(section).getByRole("button", { name: s.triggers.add }));
      await userEvent.type(within(section).getByLabelText(s.triggers.name), "Mondays");
      expect(within(section).getByLabelText(s.triggers.timeZone)).toHaveValue("UTC");
      await userEvent.type(within(section).getByRole("textbox", { name: "project" }), "RTR");
      await userEvent.click(within(section).getByRole("button", { name: s.triggers.preview }));
      expect(await within(section).findByText(s.triggers.nextRuns)).toBeInTheDocument();
      expect(await axeViolations()).toEqual([]);
      await userEvent.click(within(section).getByRole("button", { name: s.triggers.save }));
      await waitFor(() => expect(posted).toBeDefined());
      expect(posted).toMatchObject({
        name: "Mondays",
        kind: "schedule",
        schedule: { cron: "0 8 * * 1", timeZone: "UTC", noOverlap: true },
        inputs: { project: "RTR" },
      });
      expect(await within(section).findByText(/Next run/)).toBeInTheDocument();
    },
    FORM_TEST_MS,
  );

  it("show why Rotor turned one off, its last run, and refuse what the server refuses", async () => {
    await renderAt(`/workflows/${WORKFLOW.id}`, {
      answers: pageAnswers({
        [`/api/v1/workflows/${WORKFLOW.id}/triggers`]: { triggers: [TRIGGER] },
        [`POST /api/v1/triggers/${TRIGGER.id}/enable`]: () =>
          Response.json(
            { error: { code: "connections_needed", message: "This workflow uses exciter as you, so connect it first, then start the run again." } },
            { status: 409 },
          ),
      }),
    });
    const row = (await screen.findByText("Mondays")).closest("li") as HTMLElement;
    expect(within(row).getByText(TRIGGER.disabled?.message ?? "")).toBeInTheDocument();
    expect(within(row).getByRole("link")).toHaveAttribute("href", `/runs/${TRIGGER.lastFire?.runId}`);
    await userEvent.click(within(row).getByRole("checkbox", { name: s.triggers.enabled("Mondays") }));
    expect(await within(row).findByRole("alert")).toHaveTextContent("connect it first");
    expect(await axeViolations()).toEqual([]);
  });

  it("flag a workflow whose trigger Rotor turned off in the list", async () => {
    await renderAt("/workflows", {
      answers: { "/api/v1/workflows": { workflows: [WORKFLOW] }, "/api/v1/triggers": { triggers: [TRIGGER] } },
    });
    expect(await screen.findByText(s.triggers.flagNamed(WORKFLOW.name))).toBeInTheDocument();
  });
});
