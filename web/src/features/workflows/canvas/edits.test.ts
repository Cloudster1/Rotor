import { describe, expect, it } from "vitest";
import { NEW_WORKFLOW } from "../template";
import { addEdge, addNode, removeCase, removeEdge, removeNode, renameNode, setCase, setDefault, setEntry, setField } from "./edits";
import { INVALID_SPECS, VALID_SPECS } from "./fixtures";
import { placeProblems } from "./problems";
import { argvText, graphOf, nodePlace, parseSpec } from "./specDoc";

const spec = (name: string) => (VALID_SPECS.find(([n]) => n.endsWith(name)) as [string, string])[1];
const TRIAGE = spec("triage-with-approval.yaml");
const FLOW = spec("flow-style.yaml");
const CODE = spec("transform-with-code.yaml");
const read = (source: string) => {
  const parsed = parseSpec(source);
  if (!parsed) throw new Error("does not parse");
  return parsed.spec;
};
const changed = (before: string, after: string) => {
  const a = before.split("\n");
  const b = after.split("\n");
  return { removed: a.filter((l) => !b.includes(l)), added: b.filter((l) => !a.includes(l)) };
};

describe("the canvas reads a spec", () => {
  it("as the server's Spec.Graph() draws it: edges, then router cases by value, then the default", () => {
    expect(graphOf(read(TRIAGE))).toEqual({
      entry: "assess",
      nodes: [
        { id: "assess", type: "agent" },
        { id: "decide", type: "router" },
        { id: "confirm", type: "approval" },
        { id: "request", type: "tool" },
        { id: "done", type: "code" },
      ],
      edges: [
        { from: "assess", to: "decide" },
        { from: "confirm", to: "request" },
        { from: "decide", to: "done", label: "fine" },
        { from: "decide", to: "confirm", label: "review" },
      ],
    });
    expect(graphOf(read(FLOW)).edges).toEqual([
      { from: "pick", to: "read", label: "project" },
      { from: "pick", to: "who", label: "yes" },
      { from: "pick", to: "who", label: "default" },
    ]);
  });

  it("as YAML 1.2, so `on` is a key and `yes` a case, not booleans", () => {
    const pick = read(FLOW).nodes[0];
    expect(pick?.fields.on).toBe("inputs.kind");
    expect(pick?.cases).toEqual([
      ["project", "read"],
      ["yes", "who"],
    ]);
  });

  it("not at all when the YAML does not parse", () => {
    expect(parseSpec("nodes: [")).toBeUndefined();
  });

  it("and says where each node starts, for the text editor", () => {
    expect(nodePlace(TRIAGE, "decide")).toEqual({ line: 40, column: 5 });
  });
});

describe("an edit on the canvas", () => {
  it("draws an edge as the edges beside it are written, and removing it gives the text back", () => {
    const added = addEdge(TRIAGE, "request", "done");
    expect(changed(TRIAGE, added)).toEqual({ removed: [], added: ["  - { from: request, to: done }"] });
    expect(removeEdge(added, "request", "done")).toBe(TRIAGE);
  });

  it("writes the edges a spec does not have yet", () => {
    const added = addNode(NEW_WORKFLOW, "agent", "draft");
    const linked = addEdge(added, "ask", "draft");
    expect(linked.endsWith('  - id: draft\n    type: agent\n    prompt: ""\nedges:\n  - { from: ask, to: draft }\n')).toBe(true);
    expect(graphOf(read(linked)).edges).toEqual([{ from: "ask", to: "draft" }]);
  });

  it("adds a node in the style of the nodes before it", () => {
    expect(changed(FLOW, addNode(FLOW, "approval", "check")).added).toEqual(['  - {id: check, type: approval, message: ""}']);
    const code = addNode(TRIAGE, "code", "sum");
    expect(code).toBe(TRIAGE.replace("\nedges:", '\n  - id: sum\n    type: code\n    source: ""\n    output:\n      schema:\n        type: object\nedges:'));
    expect(removeNode(code, "sum")).toBe(TRIAGE);
  });

  it("removes a node with every way to and from it, and leaves the entry for the checks", () => {
    const out = removeNode(TRIAGE, "confirm");
    const after = read(out);
    expect(after.nodes.map((n) => n.id)).toEqual(["assess", "decide", "request", "done"]);
    expect(after.edges).toEqual([{ from: "assess", to: "decide" }]);
    expect(after.nodes[1]?.cases).toEqual([["fine", "done"]]);
    expect(out).toContain("Reads one requirement");
    const pickless = removeNode(FLOW, "who");
    expect(read(pickless).nodes[0]).toMatchObject({ cases: [["project", "read"]], default: undefined });
    expect(pickless).toContain("# Flow style throughout");
  });

  it("renames a node everywhere it is named, templates and router paths too, and back", () => {
    const out = renameNode(TRIAGE, "assess", "judge");
    expect(out).toContain("entry: judge\n");
    expect(out).toContain("on: nodes.judge.verdict");
    expect(out).toContain('note: "{{ nodes.judge.reason }}"');
    expect(out).toContain("- { from: judge, to: decide }");
    expect(out).toContain('state["nodes"]["assess"]');
    expect(renameNode(out, "judge", "assess")).toBe(TRIAGE);
    expect(renameNode(FLOW, "who", "whom")).toContain("cases: {project: read, yes: whom}, default: whom}");
  });

  it("sets a main field: a block of lines in block style, quoted in flow style, bare where that reads back the same", () => {
    const prompt = setField(TRIAGE, "assess", "prompt", "Read it.\nThen answer.\n");
    expect(prompt).toContain("    prompt: |\n      Read it.\n      Then answer.\n    tools: [exciter-read]");
    expect(read(prompt).nodes[0]?.fields.prompt).toBe("Read it.\nThen answer.\n");
    expect(setField(prompt, "assess", "prompt", read(TRIAGE).nodes[0]?.fields.prompt as string)).toBe(TRIAGE);
    expect(setField(FLOW, "who", "call", "get: me")).toContain('call: "get: me"}');
    const source = setField(CODE, "count", "source", "def run(state, ctx):\n    return {}\n");
    expect(read(source).nodes[1]?.fields.source).toBe("def run(state, ctx):\n    return {}\n");
    expect(source).toContain("# Counts are cheap to make again");
  });

  it("adds a field the node does not have", () => {
    const out = setField(NEW_WORKFLOW, "ask", "message", "Go on?");
    expect(out).toBe(NEW_WORKFLOW);
    const routed = setField(addNode(NEW_WORKFLOW, "router", "pick"), "pick", "default", "ask");
    expect(read(routed).nodes[1]?.default).toBe("ask");
  });

  it("adds a test node with what it needs, empty, and shows its command as one line (#138)", () => {
    const added = addNode(NEW_WORKFLOW, "test", "check");
    expect(added).toContain('  - id: check\n    type: test\n    workspace:\n      repository: ""\n');
    expect(added).toContain("    command: []\n    reports: []\n");
    expect(removeNode(added, "check")).toBe(NEW_WORKFLOW);
    const tests = read(spec("test-step.yaml")).nodes.find((n) => n.id === "test");
    expect(tests?.fields).toEqual({ command: "gotestsum --junitfile junit.xml -- ./..." });
    expect(argvText(["pytest", "-k", "sign in", ""])).toBe('pytest -k "sign in" ""');
  });

  it("sets and removes a router's cases and default, and the entry", () => {
    const added = setCase(TRIAGE, "decide", "later", "request");
    expect(changed(TRIAGE, added).added).toEqual(["      later: request"]);
    expect(removeCase(added, "decide", "later")).toBe(TRIAGE);
    expect(setCase(TRIAGE, "decide", "fine", "request")).toContain("      fine: request\n");
    const flow = setCase(FLOW, "pick", "no", "read");
    expect(flow).toContain("cases: {project: read, yes: who, no: read}");
    expect(removeCase(flow, "pick", "no")).toBe(FLOW);
    expect(setDefault(setDefault(TRIAGE, "decide", "done"), "decide", undefined)).toBe(TRIAGE);
    expect(setEntry(TRIAGE, "confirm")).toContain("entry: confirm\n");
    const fresh = addNode(NEW_WORKFLOW, "router", "pick");
    const cased = setCase(fresh, "pick", "a", "ask");
    expect(read(cased).nodes[1]?.cases).toEqual([["a", "ask"]]);
    expect(removeCase(cased, "pick", "a")).toBe(fresh);
  });
});

describe("the checks' findings", () => {
  it("land on the node or edge their path names in every invalid golden spec that parses", () => {
    for (const { name, source, expect: wanted } of INVALID_SPECS) {
      const parsed = parseSpec(source);
      if (!parsed) continue;
      const problems = wanted.map((w) => ({ path: w.path, code: w.code, line: 0, column: 0, message: w.code }));
      const placed = placeProblems(parsed.spec, problems);
      for (const p of problems) {
        const onNode = [...placed.byNode.values()].some((list) => list.includes(p));
        const about = /^\/(nodes|edges)\/\d+/.test(p.path) && parsed.spec.nodes.some((n) => n.id !== "");
        if (about && onNode) continue;
        expect(placed.elsewhere, `${name} ${p.path}`).toContain(p);
      }
    }
  });
});
