import { describe, expect, it } from "vitest";
import type { RunEvent } from "@/api/runs";
import { GRAPH_COLUMN_PX, GRAPH_ROW_PX } from "@/config";
import { layered, loopEdges } from "@/features/graph/layout";
import { entries, linksIn, nodeStates } from "./timeline";

let seq = 0;
const at = "2026-10-18T10:00:00Z";
const event = (type: string, node: string, payload: Record<string, unknown> = {}): RunEvent => ({ seq: ++seq, type, node, payload, at });

describe("a run's timeline", () => {
  it("follows each node's state from its events", () => {
    const events = [
      event("node.started", "ask"),
      event("node.finished", "ask", { ok: true }),
      event("node.started", "shape"),
      event("node.finished", "shape", { ok: false, error: "boom" }),
      event("node.started", "confirm"),
      event("interrupt", "confirm", { message: "Go?" }),
    ];
    expect(nodeStates(events)).toEqual({ ask: "done", shape: "failed", confirm: "waiting" });
  });

  it("joins a model's chunks and a node's log lines in a row, and keeps everything else apart", () => {
    const lines = entries([
      event("node.started", "draft", { type: "agent" }),
      event("llm.output", "draft", { text: "Hel" }),
      event("llm.output", "draft", { text: "lo" }),
      event("tool.called", "draft", { binding: "t", tool: "whoami", arguments: { a: 1 }, resultSummary: "bob", ok: true }),
      event("llm.output", "draft", { text: "again" }),
      event("log", "draft", { stream: "stdout", text: "one\n" }),
      event("log", "draft", { stream: "stdout", text: "two\n" }),
      event("runner.lost", "", { replaced: true }),
      event("run.finished", "", { status: "succeeded", reason: "" }),
    ]);
    expect(lines.map((l) => l.kind)).toEqual(["started", "llm", "tool", "llm", "log", "lost", "run"]);
    expect(lines[1]).toMatchObject({ text: "Hello" });
    expect(lines[2]).toMatchObject({ tool: "whoami", binding: "t", result: "bob", ok: true });
    expect(lines[4]).toMatchObject({ text: "one\ntwo\n" });
    expect(lines[5]).toMatchObject({ replaced: true });
  });

  it("says who an approval waits for and how it was decided", () => {
    const lines = entries([
      event("approval.requested", "review", { approvalId: "a1", node: "review", message: "File it?", approvers: { group: "quality" } }),
      event("approval.decided", "review", { approvalId: "a1", decision: "approve", by: { id: "u", name: "Carol" }, comment: "Fine" }),
      event("approval.requested", "again", { approvalId: "a2", approvers: { group: null } }),
      event("approval.decided", "again", { approvalId: "a2", decision: "reject", by: "Bob", comment: "" }),
    ]);
    expect(lines).toMatchObject([
      { kind: "requested", approval: "a1", group: "quality" },
      { kind: "decided", approval: "a1", decision: "approve", by: "Carol", comment: "Fine" },
      { kind: "requested", approval: "a2", group: "" },
      { kind: "decided", decision: "reject", by: "Bob" },
    ]);
  });

  it("finds the web addresses a tool's answer names, each once", () => {
    expect(linksIn('{"url": "https://exciter.example/r/RTR-1", "again": "https://exciter.example/r/RTR-1", "x": "ftp://no"}', ["exciter.example"])).toEqual([
      "https://exciter.example/r/RTR-1",
    ]);
  });

  it("links only to the run's tools, so planted content stays text", () => {
    const answer =
      '{"a": "https://Exciter.Example/r/1", "b": "https://evil.example/x", "c": "https://exciter.example@evil.example/y", "d": "https://exciter.example.evil.example/z"}';
    expect(linksIn(answer, ["exciter.example"])).toEqual(["https://Exciter.Example/r/1"]);
    expect(linksIn(answer, [])).toEqual([]);
  });

  it("says where links to other hosts were removed", () => {
    const lines = entries([
      { seq: 1, type: "link.removed", node: "draft", at: "2026-10-19T10:00:00Z", payload: { where: "output", count: 2, hosts: ["evil.example"] } },
    ]);
    expect(lines).toMatchObject([{ kind: "links", node: "draft", where: "output", count: 2, hosts: ["evil.example"] }]);
  });
});

describe("the graph's layout", () => {
  it("puts each node one layer below the nearest node leading to it, side by side within a layer", () => {
    const placed = layered({
      entry: "a",
      nodes: [
        { id: "a", type: "tool" },
        { id: "b", type: "router" },
        { id: "c", type: "code" },
        { id: "d", type: "code" },
        { id: "lonely", type: "code" },
      ],
      edges: [
        { from: "a", to: "b" },
        { from: "b", to: "c", label: "yes" },
        { from: "b", to: "d", label: "default" },
        { from: "c", to: "d" },
      ],
    });
    const at = Object.fromEntries(placed.map((p) => [p.id, p]));
    expect([at.a?.layer, at.b?.layer, at.c?.layer, at.d?.layer, at.lonely?.layer]).toEqual([0, 1, 2, 2, 3]);
    expect(at.c).toMatchObject({ x: 0, y: 2 * GRAPH_ROW_PX });
    expect(at.d).toMatchObject({ x: GRAPH_COLUMN_PX, y: 2 * GRAPH_ROW_PX });
  });

  it("puts a join below the longest branch into it, and what follows it below that (#135)", () => {
    const placed = layered({
      entry: "test",
      nodes: [
        { id: "test", type: "code" },
        { id: "code", type: "agent" },
        { id: "security", type: "agent" },
        { id: "fix", type: "agent" },
        { id: "gather", type: "code", join: true },
        { id: "done", type: "approval" },
      ],
      edges: [
        { from: "test", to: "code" },
        { from: "test", to: "security" },
        { from: "code", to: "fix" },
        { from: "fix", to: "gather" },
        { from: "security", to: "gather" },
        { from: "gather", to: "done" },
        { from: "gather", to: "test", label: "again" },
      ],
    });
    const at = Object.fromEntries(placed.map((p) => [p.id, p.layer]));
    expect(at).toEqual({ test: 0, code: 1, security: 1, fix: 2, gather: 3, done: 4 });
  });

  it("tells the edges that close a loop from those that only cross a layer", () => {
    const loops = loopEdges({
      entry: "write",
      nodes: ["write", "review", "verdict", "done", "again"].map((id) => ({ id, type: "agent" })),
      edges: [
        { from: "write", to: "review" },
        { from: "review", to: "verdict" },
        { from: "verdict", to: "done", label: "approve" },
        { from: "verdict", to: "write", label: "changes" },
        { from: "review", to: "done" },
        { from: "again", to: "again" },
      ],
    });
    expect([...loops].sort()).toEqual([3, 5]);
  });
});

describe("a loop on the timeline", () => {
  it("numbers each visit of a node that ran more than once, and no other", () => {
    const lines = entries([
      event("node.started", "write", { type: "agent", visit: 1 }),
      event("node.started", "ask", { type: "approval", visit: 1 }),
      event("node.started", "ask", { type: "approval", visit: 1 }),
      event("node.started", "write", { type: "agent", visit: 2 }),
      event("node.started", "old", { type: "tool" }),
    ]);
    expect(lines).toMatchObject([
      { node: "write", visit: 1, repeats: true },
      { node: "ask", visit: 1, repeats: false },
      { node: "ask", visit: 1, repeats: false },
      { node: "write", visit: 2, repeats: true },
      { node: "old", visit: 1, repeats: false },
    ]);
  });
});
