import { act, cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import type { Approval, ApprovalSummary } from "@/api/approvals";
import type { Run } from "@/api/runs";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { FakeEventSource } from "@/test/eventsource";
import { ME, renderAt } from "@/test/app";

beforeAll(async () => {
  await import("@/features/runs/RunRoute");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  FakeEventSource.opened = [];
});

const BOB = { id: "0190a5c4-0000-7000-8000-000000000002", name: "Bob Builder" };
const RUN_ID = "0190a5c4-0000-7000-8000-00000000cc01";

const SUMMARY: ApprovalSummary = {
  id: "0190a5c4-0000-7000-8000-00000000ab01",
  run: { id: RUN_ID, workflow: { id: "0190a5c4-0000-7000-8000-00000000bb01", name: "draft-requirements" }, startedBy: BOB },
  node: "review",
  message: "File the drafted requirement in Exciter?",
  status: "pending",
  approvers: { group: "quality" },
  canDecide: true,
  requestedAt: "2026-10-19T10:00:00Z",
  expiresAt: "2026-10-22T10:00:00Z",
  decidedAt: null,
};

const APPROVAL: Approval = {
  ...SUMMARY,
  preview: [
    { node: "file", binding: "exciter-write", tool: "exciter", call: "create_requirement", arguments: { projectKey: "WEB", title: "Login" } },
    { node: "polish", binding: "exciter-write", tool: "exciter", calls: ["update_requirement"], arguments: null },
  ],
  form: {
    schema: { type: "object", required: ["title"], properties: { title: { type: "string" }, statement: { type: "string" } } },
    values: { title: "Login", statement: "People sign in." },
  },
  decision: null,
};

const at = `/approvals/${APPROVAL.id}`;
const one = `/api/v1/approvals/${APPROVAL.id}`;
const decide = `POST /api/v1/approvals/${APPROVAL.id}/decision`;

describe("the approvals inbox", () => {
  it("lists what waits for the person, with who decides and a link to each", async () => {
    let asked: URL | undefined;
    await renderAt("/approvals", {
      answers: {
        "/api/v1/approvals": (request: Request) => {
          asked = new URL(request.url);
          return Response.json({ approvals: [SUMMARY] });
        },
      },
    });
    const list = await screen.findByRole("list", { name: s.approvals.label });
    expect(asked?.searchParams.get("waitingOnMe")).toBe("true");
    const link = within(list).getByRole("link", { name: s.approvals.title(SUMMARY.run.workflow.name) });
    expect(link).toHaveAttribute("href", at);
    expect(within(list).getByText(SUMMARY.message)).toBeInTheDocument();
    expect(within(list).getByText(new RegExp(s.approvals.byGroup("quality")))).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("lists every approval the person may see when asked, and says so when there is none", async () => {
    let asked: URL | undefined;
    await renderAt("/approvals?show=all", {
      answers: {
        "/api/v1/approvals": (request: Request) => {
          asked = new URL(request.url);
          return Response.json({ approvals: [] });
        },
      },
    });
    expect(await screen.findByRole("heading", { level: 2, name: s.approvals.allEmptyTitle })).toBeInTheDocument();
    expect(asked?.searchParams.get("status")).toBe("all");
    expect(screen.getByRole("button", { name: s.approvals.all })).toHaveAttribute("aria-pressed", "true");
    expect(await axeViolations()).toEqual([]);
  });

  it("counts what waits in the sidebar", async () => {
    await renderAt("/workflows", { answers: { "/api/v1/approvals/count": { waitingOnMe: 3 } } });
    const nav = await screen.findByRole("navigation", { name: s.shell.navigation });
    const link = await within(nav).findByRole("link", { name: new RegExp(s.approvals.badge(3)) });
    expect(within(link).getByText("3")).toHaveAttribute("data-approval-badge", "3");
    expect(await axeViolations()).toEqual([]);
  });
});

describe("one approval", () => {
  it("shows what the run is about to write and approves it with changed values and a comment", async () => {
    let body: unknown;
    // The server answers the approval as decided from then on, as a reload after the decision asks again.
    let current: Approval = APPROVAL;
    await renderAt(at, {
      answers: {
        [one]: () => Response.json(current),
        [decide]: async (request: Request) => {
          body = await request.json();
          current = {
            ...APPROVAL,
            status: "approved",
            canDecide: false,
            decision: {
              decision: "approve",
              by: { id: ME.user.id, name: ME.user.name },
              comment: "Clearer",
              values: { title: "Sign in" },
              at: "2026-10-19T11:00:00Z",
            },
          };
          return Response.json(current);
        },
      },
    });
    const preview = await screen.findByRole("region", { name: s.approvals.page.preview });
    expect(within(preview).getByText(s.approvals.page.call("file", "create_requirement"))).toBeInTheDocument();
    expect(within(preview).getByText(/"projectKey": "WEB"/)).toBeInTheDocument();
    expect(within(preview).getByText(s.approvals.page.calls("polish", "update_requirement"))).toBeInTheDocument();
    expect(within(preview).getByText(s.approvals.page.ownArguments)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    const title = screen.getByRole("textbox", { name: /title/ });
    expect(title).toHaveValue("Login");
    await userEvent.clear(title);
    await userEvent.type(title, "Sign in");
    await userEvent.type(screen.getByRole("textbox", { name: s.approvals.page.comment }), "Clearer");
    await userEvent.click(screen.getByRole("button", { name: s.approvals.page.approve }));

    await waitFor(() => expect(body).toEqual({ decision: "approve", comment: "Clearer", values: { title: "Sign in", statement: "People sign in." } }));
    expect(
      await screen.findByText(s.approvals.page.decided(s.approvals.status.approved, ME.user.name, new Date("2026-10-19T11:00:00Z").toLocaleString())),
    ).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.approvals.page.approve })).not.toBeInTheDocument();
  });

  it("asks before rejecting, and rejects without values", async () => {
    let body: unknown;
    let current: Approval = APPROVAL;
    await renderAt(at, {
      answers: {
        [one]: () => Response.json(current),
        [decide]: async (request: Request) => {
          body = await request.json();
          const by = { id: ME.user.id, name: ME.user.name };
          current = {
            ...APPROVAL,
            status: "rejected",
            canDecide: false,
            decision: { decision: "reject", by, comment: "", values: null, at: "2026-10-19T11:00:00Z" },
          };
          return Response.json(current);
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: s.approvals.page.reject }));
    const confirm = screen.getByRole("group", { name: s.approvals.page.confirmReject });
    expect(body).toBeUndefined();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(confirm).getByRole("button", { name: s.approvals.page.confirm }));
    await waitFor(() => expect(body).toEqual({ decision: "reject" }));
    expect(await screen.findByText(new RegExp(`^${s.approvals.status.rejected} by ${ME.user.name}`))).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.approvals.page.reject })).not.toBeInTheDocument();
  });

  it("says an approval that ended without a decision is no longer anyone's to decide", async () => {
    await renderAt(at, { answers: { [one]: { ...APPROVAL, status: "expired", canDecide: false } } });
    expect(await screen.findByText(s.approvals.page.ended(s.approvals.status.expired))).toBeInTheDocument();
  });

  it("puts the server's word on a value beside its field", async () => {
    await renderAt(at, {
      answers: {
        [one]: APPROVAL,
        [decide]: () =>
          Response.json(
            { error: { code: "values_invalid", message: "Some fields need attention.", fields: { "values/title": "title is longer than 200 characters." } } },
            { status: 422 },
          ),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: s.approvals.page.approve }));
    expect(await screen.findByText("title is longer than 200 characters.")).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: /title/ })).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("says why when the decision is refused", async () => {
    const message = "Carol Clerk approved this at 10:30. It no longer waits for a decision.";
    await renderAt(at, {
      answers: { [one]: APPROVAL, [decide]: () => Response.json({ error: { code: "approval_decided", message } }, { status: 409 }) },
    });
    await userEvent.click(await screen.findByRole("button", { name: s.approvals.page.approve }));
    expect(await screen.findByRole("alert")).toHaveTextContent(message);
  });

  it("only shows an approval someone else decides", async () => {
    await renderAt(at, { answers: { [one]: { ...APPROVAL, canDecide: false } } });
    expect(await screen.findByText(s.approvals.page.notYours)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.approvals.page.approve })).not.toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("shows how a decided approval was decided", async () => {
    const decided: Approval = {
      ...APPROVAL,
      status: "approved",
      canDecide: false,
      decidedAt: "2026-10-19T11:00:00Z",
      decision: { decision: "approve", by: { id: "c", name: "Carol Clerk" }, comment: "Good to go", values: { title: "Sign in" }, at: "2026-10-19T11:00:00Z" },
    };
    await renderAt(at, { answers: { [one]: decided } });
    expect(await screen.findByText("Good to go")).toBeInTheDocument();
    expect(screen.getByText(/"title": "Sign in"/)).toBeInTheDocument();
    expect(screen.getByText(/Approved by Carol Clerk/)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("says so when there is no such approval", async () => {
    await renderAt(at);
    expect(await screen.findByRole("alert")).toHaveTextContent(s.approvals.page.notFound);
  });
});

describe("a waiting run", () => {
  const WAITING: Run = {
    id: RUN_ID,
    workflow: SUMMARY.run.workflow,
    version: 1,
    status: "waiting",
    startedBy: BOB,
    reason: "",
    inputs: {},
    output: null,
    createdAt: "2026-10-19T10:00:00Z",
    updatedAt: "2026-10-19T10:00:00Z",
    startedAt: "2026-10-19T10:00:01Z",
    finishedAt: null,
    ended: false,
    approvalId: APPROVAL.id,
    trigger: null,
  };

  it("leads to the approval it waits for, from its page and its timeline", async () => {
    await renderAt(`/runs/${RUN_ID}`, { answers: { [`/api/v1/runs/${RUN_ID}`]: WAITING } });
    const panel = await screen.findByText(s.approvals.run.waits);
    expect(within(panel.parentElement as HTMLElement).getByRole("link", { name: s.approvals.run.open })).toHaveAttribute("href", at);
    act(() =>
      FakeEventSource.last().send("approval.requested", {
        seq: 1,
        type: "approval.requested",
        node: "review",
        payload: { approvalId: APPROVAL.id, node: "review", message: "File it?", approvers: { group: "quality" } },
        at: "2026-10-19T10:00:02Z",
      }),
    );
    const timeline = screen.getByRole("region", { name: s.runs.live.timeline });
    expect(await within(timeline).findByText(new RegExp(s.approvals.live.requested("review", s.approvals.live.group("quality"))))).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });
});
