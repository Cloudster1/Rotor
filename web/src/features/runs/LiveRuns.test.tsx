import { act, cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import type { Run } from "@/api/runs";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { FakeEventSource } from "@/test/eventsource";
import { ME, renderAt } from "@/test/app";

beforeAll(async () => {
  await import("./RunRoute");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  FakeEventSource.opened = [];
});

const RUN: Run = {
  id: "0190a5c4-0000-7000-8000-00000000dd01",
  workflow: { id: "0190a5c4-0000-7000-8000-00000000bb01", name: "triage" },
  version: 2,
  status: "running",
  startedBy: { id: ME.user.id, name: ME.user.name },
  reason: "",
  inputs: {},
  output: null,
  createdAt: "2026-10-18T10:00:00Z",
  updatedAt: "2026-10-18T10:00:00Z",
  startedAt: "2026-10-18T10:00:01Z",
  finishedAt: null,
  ended: false,
  approvalId: null,
  trigger: null,
  usage: { inputTokens: 0, outputTokens: 0, cost: 0 },
  linkHosts: ["exciter.example"],
  graph: {
    entry: "ask",
    nodes: [
      { id: "ask", type: "tool" },
      { id: "draft", type: "agent" },
    ],
    edges: [{ from: "ask", to: "draft" }],
  },
};

let seq = 0;
function send(type: string, node: string, payload: Record<string, unknown> = {}) {
  act(() => FakeEventSource.last().send(type, { seq: ++seq, type, node, payload, at: "2026-10-18T10:00:02Z" }));
}

describe("a run, live", () => {
  it("lights its graph up and tells what happened as the events arrive, until it ends", async () => {
    seq = 0;
    let current: Run = RUN;
    let asked = 0;
    await renderAt(`/runs/${RUN.id}`, {
      answers: {
        [`/api/v1/runs/${RUN.id}`]: () => {
          asked += 1;
          return Response.json(current);
        },
      },
    });
    await screen.findByRole("region", { name: s.runs.live.graph });
    expect(FakeEventSource.last().url).toBe(`/api/v1/runs/${RUN.id}/events`);
    const node = (id: string) => document.querySelector(`[data-node="${id}"]`);
    await waitFor(() => expect(node("ask")).toHaveAttribute("data-node-state", "pending"));

    send("node.started", "ask", { type: "tool" });
    await waitFor(() => expect(node("ask")).toHaveAttribute("data-node-state", "running"));
    send("tool.called", "ask", {
      binding: "t",
      tool: "create_requirement",
      arguments: { title: "Login" },
      resultSummary: '{"url": "https://exciter.example/r/RTR-7", "planted": "https://evil.example/x"}',
      ok: true,
    });
    send("node.finished", "ask", { ok: true });
    send("node.started", "draft", { type: "agent" });
    send("llm.output", "draft", { text: "Drafting " });
    send("llm.output", "draft", { text: "the requirement." });
    // The same event again, as after a reconnect, is shown once.
    act(() => FakeEventSource.last().send("node.started", { seq: 4, type: "node.started", node: "draft", payload: {}, at: "" }));

    const timeline = screen.getByRole("region", { name: s.runs.live.timeline });
    expect(await within(timeline).findByText("Drafting the requirement.")).toBeInTheDocument();
    expect(within(timeline).getAllByText(s.runs.live.started("draft", "agent"))).toHaveLength(1);
    expect(node("ask")).toHaveAttribute("data-node-state", "done");
    await userEvent.click(within(timeline).getByText(s.runs.live.called("create_requirement", "t")));
    expect(within(timeline).getByRole("link", { name: "https://exciter.example/r/RTR-7" })).toHaveAttribute("href", "https://exciter.example/r/RTR-7");
    // A host none of the run's tools has is never a link (#40).
    expect(within(timeline).queryByRole("link", { name: "https://evil.example/x" })).toBeNull();
    send("link.removed", "draft", { where: "output", count: 1, hosts: ["evil.example"] });
    await waitFor(() =>
      expect(timeline.querySelector("[data-links-removed]")?.textContent).toBe(
        `${s.untrusted.linksRemovedWhere.output}: ${s.untrusted.linksRemoved(1, ["evil.example"])}`,
      ),
    );
    expect(await axeViolations()).toEqual([]);

    const before = asked;
    current = { ...RUN, status: "succeeded", ended: true, finishedAt: "2026-10-18T10:01:00Z", output: { draft: { title: "Login" } } };
    send("node.finished", "draft", { ok: true });
    send("run.finished", "", { status: "succeeded", reason: "" });
    expect(await screen.findByText(s.runs.status.succeeded, { selector: "[data-run-status]" })).toBeInTheDocument();
    expect(asked).toBeGreaterThan(before);
    expect(FakeEventSource.last().readyState).toBe(FakeEventSource.CLOSED);
    expect(within(timeline).getByText(s.runs.live.ended(s.runs.status.succeeded))).toBeInTheDocument();
  });
});

describe("the runs list", () => {
  const OTHER: Run = { ...RUN, id: "0190a5c4-0000-7000-8000-00000000dd02", status: "failed", reason: "tool_error", ended: true };

  it("lists the runs and narrows them by state, workflow and who started them", async () => {
    const asked: URLSearchParams[] = [];
    const { router } = await renderAt("/runs", {
      answers: {
        "/api/v1/runs": (request: Request) => {
          const query = new URL(request.url).searchParams;
          asked.push(query);
          return Response.json({ runs: query.get("status") === "failed" ? [OTHER] : [RUN, OTHER] });
        },
        "/api/v1/workflows": { workflows: [] },
      },
    });
    const runs = () => screen.getByRole("list", { name: s.runs.list.label });
    await waitFor(() => expect(within(runs()).getAllByRole("listitem")).toHaveLength(2));
    expect(within(runs()).getAllByRole("link", { name: s.runs.page.title("triage") })[0]).toHaveAttribute("href", `/runs/${RUN.id}`);

    await userEvent.selectOptions(screen.getByLabelText(s.runs.list.status), "failed");
    await waitFor(() => expect(router.state.location.search).toMatchObject({ status: "failed" }));
    await waitFor(() => expect(within(runs()).getAllByRole("listitem")).toHaveLength(1));
    expect(asked.at(-1)?.getAll("status")).toEqual(["failed"]);

    await userEvent.click(screen.getByLabelText(s.runs.list.mine));
    await waitFor(() => expect(asked.at(-1)?.get("mine")).toBe("true"));
    expect(await axeViolations()).toEqual([]);
  });

  it("says when nothing matches, and when nothing has run yet", async () => {
    await renderAt("/runs?status=expired");
    expect(await screen.findByText(s.runs.list.noneTitle)).toBeInTheDocument();
    cleanup();
    await renderAt("/runs");
    expect(await screen.findByText(s.runs.emptyTitle)).toBeInTheDocument();
  });
});
