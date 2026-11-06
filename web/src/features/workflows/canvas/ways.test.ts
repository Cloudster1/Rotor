import { describe, expect, it } from "vitest";
import { NEW_WORKFLOW } from "../template";
import { VALID_SPECS } from "./fixtures";
import { graphOf, parseSpec } from "./specDoc";
import { addAfter, canConnect, connect, disconnect, freshId, insertBetween, removeAll, type Way, wayOf, wayOut } from "./ways";

const spec = (name: string) => (VALID_SPECS.find(([n]) => n.endsWith(name)) as [string, string])[1];
const TRIAGE = spec("triage-with-approval.yaml");
const FLOW = spec("flow-style.yaml");
const read = (source: string) => {
  const parsed = parseSpec(source);
  if (!parsed) throw new Error("does not parse");
  return parsed.spec;
};
const edges = (source: string) => graphOf(read(source)).edges.map((e) => `${e.from}>${e.to}:${e.kind}${e.label ? `:${e.label}` : ""}`);

describe("the ways between nodes", () => {
  const ways: [string, string, string, string, Way][] = [
    ["an edge", TRIAGE, "request", "done", { kind: "edge" }],
    ["a case", TRIAGE, "decide", "request", { kind: "case", value: "later" }],
    ["a default", TRIAGE, "decide", "request", { kind: "default" }],
    ["an onExhausted", TRIAGE, "confirm", "assess", { kind: "exhausted" }],
    ["a case in flow style", FLOW, "pick", "read", { kind: "case", value: "maybe" }],
  ];

  it.each(ways)("draws %s and takes it back to the text it was", (_, source, from, to, way) => {
    const drawn = connect(source, from, to, way);
    const added = graphOf(read(drawn)).edges.find((e) => e.from === from && e.to === to && wayOf(e).kind === way.kind);
    expect(added).toBeDefined();
    expect(disconnect(drawn, from, to, way)).toBe(source);
  });

  it("knows each drawn edge's way, and a run's edge without a kind as an edge", () => {
    const drawn = graphOf(read(FLOW)).edges;
    expect(drawn.map(wayOf)).toEqual([{ kind: "case", value: "project" }, { kind: "case", value: "yes" }, { kind: "default" }]);
    expect(wayOf({ from: "a", to: "b", label: "default" })).toEqual({ kind: "edge" });
  });

  it("takes a router's default as a default even when a case is called default", () => {
    const cased = connect(FLOW, "pick", "read", { kind: "case", value: "default" });
    const ways = graphOf(read(cased))
      .edges.filter((e) => e.label === "default")
      .map(wayOf);
    expect(ways).toEqual([{ kind: "case", value: "default" }, { kind: "default" }]);
  });
});

describe("a new way out of a node", () => {
  it("is an edge from a node, the default from a router without one, else a case", () => {
    expect(wayOut(read(TRIAGE), "assess")).toEqual({ kind: "edge" });
    expect(wayOut(read(TRIAGE), "decide")).toEqual({ kind: "default" });
    expect(wayOut(read(FLOW), "pick")).toBe("case");
  });

  it("is never to the node itself, nor an edge twice, but may close a loop", () => {
    const triage = read(TRIAGE);
    expect(canConnect(triage, "assess", "assess")).toBe(false);
    expect(canConnect(triage, "assess", "decide")).toBe(false);
    expect(canConnect(triage, "request", "assess")).toBe(true);
    expect(canConnect(triage, "decide", "done")).toBe(true);
    expect(canConnect(triage, "assess", "nowhere")).toBe(false);
  });
});

describe("a new node", () => {
  it("takes the first free id of its kind", () => {
    expect(freshId("agent", [])).toBe("agent");
    expect(freshId("agent", ["agent", "agent-2", "tool"])).toBe("agent-3");
  });

  it("is led to from the node it follows", () => {
    const out = addAfter(TRIAGE, "agent", "agent", { id: "request", way: { kind: "edge" } });
    expect(edges(out)).toContain("request>agent:edge");
    expect(read(out).entry).toBe("assess");
  });

  it("becomes a router's default, or one of its cases", () => {
    expect(edges(addAfter(TRIAGE, "code", "code", { id: "decide", way: { kind: "default" } }))).toContain("decide>code:default:default");
    expect(edges(addAfter(FLOW, "code", "code", { id: "pick", way: { kind: "case", value: "no" } }))).toContain("pick>code:case:no");
  });

  it("is where a workflow that starts nowhere starts, and nothing else moves the entry", () => {
    const empty = NEW_WORKFLOW.replace(/entry: \S+/, "entry: gone");
    expect(read(addAfter(empty, "agent", "agent")).entry).toBe("agent");
    expect(read(addAfter(TRIAGE, "agent", "agent")).entry).toBe("assess");
  });

  it("goes between the two ends of an edge, a case, a default and an onExhausted", () => {
    const edge = insertBetween(TRIAGE, "agent", "agent", "assess", "decide", { kind: "edge" });
    expect(edges(edge)).toEqual(expect.arrayContaining(["assess>agent:edge", "agent>decide:edge"]));
    expect(edges(edge)).not.toContain("assess>decide:edge");

    const cased = insertBetween(TRIAGE, "agent", "agent", "decide", "done", { kind: "case", value: "fine" });
    expect(edges(cased)).toEqual(expect.arrayContaining(["decide>agent:case:fine", "agent>done:edge"]));

    const byDefault = insertBetween(FLOW, "code", "code", "pick", "who", { kind: "default" });
    expect(edges(byDefault)).toEqual(expect.arrayContaining(["pick>code:default:default", "code>who:edge"]));

    const looped = connect(TRIAGE, "confirm", "assess", { kind: "exhausted" });
    const exhausted = insertBetween(looped, "approval", "approval", "confirm", "assess", { kind: "exhausted" });
    expect(edges(exhausted)).toEqual(expect.arrayContaining(["confirm>approval:exhausted:onExhausted", "approval>assess:edge"]));
  });

  it("goes on by its default when the new node is a router", () => {
    const out = insertBetween(TRIAGE, "router", "router", "assess", "decide", { kind: "edge" });
    expect(edges(out)).toEqual(expect.arrayContaining(["assess>router:edge", "router>decide:default:default"]));
  });
});

describe("removing what is selected", () => {
  it("takes ways and nodes in one change, and leaves the ways of a removed node to it", () => {
    const graph = graphOf(read(TRIAGE));
    const toDone = graph.edges.findIndex((e) => e.to === "done");
    const out = removeAll(TRIAGE, graph, { nodes: ["confirm"], edges: [toDone] });
    expect(edges(out)).toEqual(["assess>decide:edge"]);
    expect(read(out).nodes.map((n) => n.id)).not.toContain("confirm");
  });
});
