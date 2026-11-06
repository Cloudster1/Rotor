import type { Graph } from "@/features/graph/layout";
import * as edit from "./edits";
import { NODE_ID, type NodeType, parseSpec, type SpecShape } from "./specDoc";

/** One way out of a node, as the YAML writes it: a top-level edge, a router's case or default, or onExhausted. */
export type Way = { kind: "edge" } | { kind: "case"; value: string } | { kind: "default" } | { kind: "exhausted" };

const EDGE: Way = { kind: "edge" };
const DEFAULT: Way = { kind: "default" };

/** The way a drawn edge stands for; an edge drawn without a kind, as a run's graph is, is a top-level edge. */
export function wayOf(drawn: Graph["edges"][number]): Way {
  if (drawn.kind === "case") return { kind: "case", value: drawn.label ?? "" };
  if (drawn.kind === "default") return DEFAULT;
  if (drawn.kind === "exhausted") return { kind: "exhausted" };
  return EDGE;
}

/** Writes a way from one node to another; a case, default or onExhausted that is there already now leads to `to`. */
export function connect(source: string, from: string, to: string, way: Way): string {
  switch (way.kind) {
    case "edge":
      return edit.addEdge(source, from, to);
    case "case":
      return edit.setCase(source, from, way.value, to);
    case "default":
      return edit.setDefault(source, from, to);
    case "exhausted":
      return edit.setField(source, from, "onExhausted", to);
  }
}

/** Takes a way out of the YAML again. */
export function disconnect(source: string, from: string, to: string, way: Way): string {
  switch (way.kind) {
    case "edge":
      return edit.removeEdge(source, from, to);
    case "case":
      return edit.removeCase(source, from, way.value);
    case "default":
      return edit.setDefault(source, from, undefined);
    case "exhausted":
      return edit.clearField(source, from, "onExhausted");
  }
}

/** The first free id for a new node of a kind: the kind itself, then agent-2, agent-3 and on. */
export function freshId(type: NodeType, taken: readonly string[]): string {
  const used = new Set(taken);
  if (!used.has(type)) return type;
  for (let n = 2; ; n++) {
    const id = `${type}-${n}`;
    if (!used.has(id) && NODE_ID.test(id)) return id;
  }
}

/** What a new way out of a node becomes: an edge, a router's default while it has none, or else a case that needs a value.
 * A router never takes a top-level edge; the server refuses one (router_edges). */
export function wayOut(spec: SpecShape, from: string): Way | "case" {
  const node = spec.nodes.find((n) => n.id === from);
  if (node?.type !== "router") return EDGE;
  return node.default ? "case" : DEFAULT;
}

/** Whether the canvas draws a new way between two nodes: never a node to itself, and never an edge twice.
 * A way back to an earlier node is a loop, which a workflow may have. */
export function canConnect(spec: SpecShape, from: string, to: string): boolean {
  if (from === to) return false;
  const ids = new Set(spec.nodes.map((n) => n.id));
  if (!ids.has(from) || !ids.has(to)) return false;
  if (wayOut(spec, from) !== EDGE) return true;
  return !spec.edges.some((e) => e.from === from && e.to === to);
}

/** Adds a node led to from another one; without one, a workflow that starts nowhere starts at the new node. */
export function addAfter(source: string, type: NodeType, id: string, from?: { id: string; way: Way }): string {
  const added = edit.addNode(source, type, id);
  if (from) return connect(added, from.id, id, from.way);
  const spec = parseSpec(source)?.spec;
  return spec?.nodes.some((n) => n.id === spec.entry) ? added : edit.setEntry(added, id);
}

/** Puts a new node on a way, so from leads to it and it leads on to `to`; a new router goes on by its default. */
export function insertBetween(source: string, type: NodeType, id: string, from: string, to: string, way: Way): string {
  let out = edit.addNode(source, type, id);
  if (way.kind === "edge") out = edit.addEdge(edit.removeEdge(out, from, to), from, id);
  else out = connect(out, from, id, way);
  return connect(out, id, to, type === "router" ? DEFAULT : EDGE);
}

/** Removes ways and nodes in one change, so one undo brings all of them back. */
export function removeAll(source: string, graph: Graph, what: { nodes: readonly string[]; edges: readonly number[] }): string {
  let out = source;
  for (const i of what.edges) {
    const drawn = graph.edges[i];
    if (drawn && !what.nodes.includes(drawn.from) && !what.nodes.includes(drawn.to)) out = disconnect(out, drawn.from, drawn.to, wayOf(drawn));
  }
  for (const id of what.nodes) out = edit.removeNode(out, id);
  return out;
}
