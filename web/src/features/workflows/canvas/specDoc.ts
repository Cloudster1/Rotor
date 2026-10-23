import { type Document, isMap, isSeq, LineCounter, parseDocument, type YAMLMap, type YAMLSeq } from "yaml";
import type { Graph } from "@/features/graph/layout";

/** The kinds of node a workflow has, as api/workflow.v1.schema.json names them. */
export const NODE_TYPES = ["agent", "tool", "code", "approval", "router"] as const;
export type NodeType = (typeof NODE_TYPES)[number];

/** A node's id as the schema allows it. */
export const NODE_ID = /^[a-z][a-z0-9_-]{0,62}$/;

/** The fields the canvas edits on each kind of node; every other field is written in the YAML. */
export const MAIN_FIELDS: Record<NodeType, readonly string[]> = {
  agent: ["prompt", "role"],
  tool: ["tool", "call"],
  code: ["source"],
  approval: ["message"],
  router: ["on"],
};

/** The main fields a node may leave out, which an emptied field takes out of the YAML (#133). */
export const OPTIONAL_FIELDS: readonly string[] = ["role"];

/** A model as a workflow names it; no provider is whichever enabled provider lists the name. */
export interface ModelRef {
  name: string;
  provider: string;
}

/** A node as the canvas reads it: its id and type, a router's ways, its main fields as text, and an agent's model. */
export interface SpecNode {
  id: string;
  type: string;
  cases: [string, string][];
  default?: string;
  /** Where the run goes past the node's last visit (#134). */
  onExhausted?: string;
  /** Whether the node waits for every branch into it, set in the YAML as join: all (#135). */
  join?: boolean;
  fields: Record<string, string>;
  model?: ModelRef;
}

/** What the canvas reads of a spec; the YAML holds the rest. */
export interface SpecShape {
  entry: string;
  nodes: SpecNode[];
  edges: { from: string; to: string }[];
  bindings: string[];
}

/** A spec the canvas can draw: the YAML document it edits and what it reads of it. */
export interface Parsed {
  doc: Document.Parsed;
  spec: SpecShape;
}

const text = (v: unknown): string => (typeof v === "string" ? v : v === undefined || v === null ? "" : String(v));
const record = (v: unknown): Record<string, unknown> => (v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {});

/** Reads YAML as Go and Python do (YAML 1.2, so `on` and `yes` stay text); undefined when it does not parse. */
export function parseSpec(source: string): Parsed | undefined {
  const doc = parseDocument(source, { version: "1.2" });
  if (doc.errors.length > 0 || !isMap(doc.contents)) return undefined;
  const js = record(doc.toJS());
  const nodes = (Array.isArray(js.nodes) ? js.nodes : []).map((raw): SpecNode => {
    const n = record(raw);
    const fields: Record<string, string> = {};
    for (const key of MAIN_FIELDS[n.type as NodeType] ?? []) fields[key] = text(n[key]);
    const model = record(n.model);
    return {
      id: text(n.id),
      type: text(n.type),
      cases: Object.entries(record(n.cases)).map(([value, to]) => [value, text(to)]),
      default: n.default === undefined ? undefined : text(n.default),
      onExhausted: n.onExhausted === undefined ? undefined : text(n.onExhausted),
      ...(n.join === "all" ? { join: true } : {}),
      fields,
      model: n.model === undefined ? undefined : { name: text(model.name), provider: text(model.provider) },
    };
  });
  const edges = (Array.isArray(js.edges) ? js.edges : []).map((raw) => {
    const e = record(raw);
    return { from: text(e.from), to: text(e.to) };
  });
  return { doc, spec: { entry: text(js.entry), nodes, edges, bindings: Object.keys(record(js.tools)) } };
}

/** The graph the server's Spec.Graph() draws: nodes in order with their join, the edges, each router's cases by value
 * and its default, then each node's onExhausted. */
export function graphOf(spec: SpecShape): Graph {
  const seen = new Set<string>();
  const nodes = spec.nodes
    .filter((n) => n.id !== "" && !seen.has(n.id) && seen.add(n.id))
    .map((n) => ({ id: n.id, type: n.type, ...(n.join ? { join: true } : {}) }));
  const known = (id: string) => seen.has(id);
  const edges: Graph["edges"] = spec.edges.filter((e) => known(e.from) && known(e.to)).map((e) => ({ from: e.from, to: e.to }));
  for (const n of spec.nodes) {
    if (n.type !== "router" || !known(n.id)) continue;
    const cases = [...n.cases].sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0));
    for (const [value, to] of cases) if (known(to)) edges.push({ from: n.id, to, label: value });
    if (n.default && known(n.default)) edges.push({ from: n.id, to: n.default, label: "default" });
  }
  for (const n of spec.nodes) if (n.onExhausted && known(n.id) && known(n.onExhausted)) edges.push({ from: n.id, to: n.onExhausted, label: "onExhausted" });
  return { entry: spec.entry, nodes, edges };
}

/** The nodes sequence and the map of one node in it, by id; undefined when there is none. */
export function nodeMap(doc: Document.Parsed, id: string): { seq: YAMLSeq; map: YAMLMap; index: number } | undefined {
  const seq = doc.get("nodes", true);
  if (!isSeq(seq)) return undefined;
  const index = seq.items.findIndex((item) => isMap(item) && text(item.get("id")) === id);
  const map = seq.items[index];
  return isMap(map) ? { seq, map, index } : undefined;
}

/** Where a node starts in the YAML, from line and column 1, for opening it in the text editor. */
export function nodePlace(source: string, id: string): { line: number; column: number } | undefined {
  const counter = new LineCounter();
  const doc = parseDocument(source, { version: "1.2", lineCounter: counter });
  const found = nodeMap(doc, id);
  const start = found?.map.range?.[0];
  if (start === undefined) return undefined;
  const { line, col } = counter.linePos(start);
  return { line, column: col };
}
