import fc from "fast-check";
import { describe, expect, it } from "vitest";
import { addEdge, addNode, removeCase, removeEdge, removeNode, renameNode, setCase, setDefault, setEntry, setField } from "./edits";
import { VALID_SPECS } from "./fixtures";
import { graphOf, MAIN_FIELDS, NODE_TYPES, type NodeType, parseSpec, type SpecShape } from "./specDoc";

// Over every golden spec: an edit and its inverse give the text back byte for byte, any edits draw what the same edits
// draw on a plain model, and the comments they do not reach stay.

const read = (source: string): SpecShape => {
  const parsed = parseSpec(source);
  if (!parsed) throw new Error(`does not parse:\n${source}`);
  return parsed.spec;
};

/** One step of a random edit session, its choices resolved against the spec it meets. */
interface Step {
  kind: "edge" | "node" | "rename" | "entry" | "case" | "default" | "field" | "remove";
  a: number;
  b: number;
  word: string;
  type: NodeType;
  value: string;
}

const TRICKY = [
  "",
  "yes",
  "1",
  "on",
  "a: b",
  "#hash",
  "- dash",
  "{{ nodes.x.y }}",
  "x\ny\n",
  "x\n\ny",
  "\nlead",
  "end\n\n",
  "  spaced",
  "tab\there",
  "'single'",
  '"double"',
  "\u00fcn\u00efcode",
  "a, b",
];

const step = fc.record<Step>({
  kind: fc.constantFrom("edge", "node", "rename", "entry", "case", "default", "field", "remove"),
  a: fc.nat(),
  b: fc.nat(),
  word: fc.stringMatching(/^[a-z][a-z0-9_-]{0,8}$/),
  type: fc.constantFrom(...NODE_TYPES),
  value: fc.oneof(fc.constantFrom(...TRICKY), fc.string(), fc.string({ unit: "grapheme" }), fc.string({ unit: "binary" })),
});

const clone = (s: SpecShape): SpecShape => structuredClone(s);
const pick = <T>(items: T[], n: number): T | undefined => (items.length === 0 ? undefined : items[n % items.length]);
const swap = (s: string, from: string, to: string) => (s === from ? to : s);
const refs = (from: string, to: string) => (text: string) => text.replace(new RegExp(`(\\{\\{[^}]*?\\bnodes\\.)${from}(?![a-z0-9_-])`, "g"), `$1${to}`);

/** What a step does to the plain model, and the text edit and its inverse; undefined when it does not apply. */
function plan(source: string, model: SpecShape, s: Step) {
  const ids = model.nodes.map((n) => n.id);
  const a = pick(ids, s.a);
  const b = pick(ids, s.b);
  const fresh = !source.includes(s.word);
  const node = model.nodes.find((n) => n.id === a);
  const routers = model.nodes.filter((n) => n.type === "router");
  const router = pick(routers, s.a);
  switch (s.kind) {
    case "edge": {
      if (!a || !b || model.edges.some((e) => e.from === a && e.to === b)) return undefined;
      return {
        edit: (t: string) => addEdge(t, a, b),
        undo: (t: string) => removeEdge(t, a, b),
        model: (m: SpecShape) => m.edges.push({ from: a, to: b }),
      };
    }
    case "node": {
      if (!fresh) return undefined;
      return {
        edit: (t: string) => addNode(t, s.type, s.word),
        undo: (t: string) => removeNode(t, s.word),
        model: (m: SpecShape) => {
          const fields = Object.fromEntries(MAIN_FIELDS[s.type].map((k) => [k, ""]));
          m.nodes.push({ id: s.word, type: s.type, cases: [], default: undefined, fields });
        },
      };
    }
    case "rename": {
      if (!a || !fresh) return undefined;
      return {
        edit: (t: string) => renameNode(t, a, s.word),
        undo: (t: string) => renameNode(t, s.word, a),
        model: (m: SpecShape) => {
          m.entry = swap(m.entry, a, s.word);
          m.edges = m.edges.map((e) => ({ from: swap(e.from, a, s.word), to: swap(e.to, a, s.word) }));
          for (const n of m.nodes) {
            n.id = swap(n.id, a, s.word);
            n.cases = n.cases.map(([v, to]) => [v, swap(to, a, s.word)]);
            if (n.default !== undefined) n.default = swap(n.default, a, s.word);
            for (const k of Object.keys(n.fields)) n.fields[k] = refs(a, s.word)(n.fields[k] as string);
            if (n.type === "router" && n.fields.on !== undefined)
              n.fields.on = n.fields.on.replace(new RegExp(`^nodes\\.${a}(?![a-z0-9_-])`), `nodes.${s.word}`);
          }
        },
      };
    }
    case "entry": {
      if (!a || model.entry === a) return undefined;
      const before = model.entry;
      return { edit: (t: string) => setEntry(t, a), undo: (t: string) => setEntry(t, before), model: (m: SpecShape) => (m.entry = a) };
    }
    case "case": {
      if (!router || !b || !fresh) return undefined;
      return {
        edit: (t: string) => setCase(t, router.id, s.word, b),
        undo: (t: string) => removeCase(t, router.id, s.word),
        model: (m: SpecShape) => m.nodes.find((n) => n.id === router.id)?.cases.push([s.word, b]),
      };
    }
    case "default": {
      if (!router || !b || router.default !== undefined) return undefined;
      return {
        edit: (t: string) => setDefault(t, router.id, b),
        undo: (t: string) => setDefault(t, router.id, undefined),
        model: (m: SpecShape) => {
          const r = m.nodes.find((n) => n.id === router.id);
          if (r) r.default = b;
        },
      };
    }
    case "field": {
      const key = node ? pick([...(MAIN_FIELDS[node.type as NodeType] ?? [])], s.b) : undefined;
      if (!node || !key) return undefined;
      const before = node.fields[key] as string;
      return {
        edit: (t: string) => setField(t, node.id, key, s.value),
        undo: (t: string) => setField(t, node.id, key, before),
        model: (m: SpecShape) => {
          const n = m.nodes.find((x) => x.id === node.id);
          if (n) n.fields[key] = s.value;
        },
      };
    }
    case "remove": {
      if (!a || model.nodes.length < 2) return undefined;
      return {
        edit: (t: string) => removeNode(t, a),
        undo: undefined,
        model: (m: SpecShape) => {
          m.nodes = m.nodes.filter((n) => n.id !== a);
          m.edges = m.edges.filter((e) => e.from !== a && e.to !== a);
          for (const n of m.nodes) {
            n.cases = n.cases.filter(([, to]) => to !== a);
            if (n.default === a) n.default = undefined;
          }
        },
      };
    }
  }
}

/** What the canvas and the model must agree on: the graph, each node's main fields and the entry. */
function drawn(spec: SpecShape) {
  return {
    graph: graphOf(spec),
    fields: spec.nodes.map((n) => [n.id, n.fields]),
    cases: spec.nodes.map((n) => [n.id, [...n.cases].sort(), n.default ?? null]),
  };
}

// Each spec gets dozens of random sessions, each a few dozen parses; a busy machine needs longer than the default.
const PROPERTY_TIMEOUT_MS = 120_000;

const comments = (source: string) => source.split("\n").filter((l) => /^\s*#/.test(l));

describe.each(VALID_SPECS)("the canvas over %s", (_, original) => {
  it("leaves a spec byte for byte as it was when an edit changes nothing", () => {
    const spec = read(original);
    expect(setEntry(original, spec.entry)).toBe(original);
    for (const n of spec.nodes) expect(renameNode(original, n.id, n.id)).toBe(original);
  });

  it(
    "gives the text back after an edit and its inverse, and draws what the same edits draw on a plain model",
    () => {
      fc.assert(
        fc.property(fc.array(step, { maxLength: 8 }), (steps) => {
          let text = original;
          let model = read(original);
          for (const s of steps) {
            const p = plan(text, model, s);
            if (!p) continue;
            const next = p.edit(text);
            if (p.undo) {
              const back = p.undo(next);
              // A value the person wrote another way than the canvas writes it comes back the same, not the same text.
              if (s.kind === "field") expect(drawn(read(back))).toEqual(drawn(model));
              else expect(back).toBe(text);
            }
            const changed = clone(model);
            p.model(changed);
            expect(drawn(read(next))).toEqual(drawn(changed));
            const removedNode = s.kind === "remove" ? (pick(model.nodes, s.a)?.id ?? "") : "";
            if (!removedNode) for (const c of comments(text)) expect(next).toContain(c);
            text = next;
            model = changed;
          }
        }),
        { numRuns: 60 },
      );
    },
    PROPERTY_TIMEOUT_MS,
  );

  it("writes a main field back as it was written when the field was written as the canvas writes it", () => {
    const spec = read(original);
    for (const n of spec.nodes) {
      for (const [key, value] of Object.entries(n.fields)) {
        if (value === "") continue;
        const changed = setField(original, n.id, key, `${value} changed`);
        const back = setField(changed, n.id, key, value);
        expect(read(back).nodes.find((x) => x.id === n.id)?.fields[key]).toBe(value);
        if (original.includes(`${key}: ${value}\n`) || value.includes("\n")) expect(back).toBe(original);
      }
    }
  });
});
