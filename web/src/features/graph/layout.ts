import { GRAPH_COLUMN_PX, GRAPH_ROW_PX } from "@/config";

/** A workflow's nodes and the ways between them, entry first, as the server's Spec.Graph() draws it. */
export interface Graph {
  entry: string;
  /** join marks a node that waits for every branch into it (#135). */
  nodes: { id: string; type: string; join?: boolean }[];
  edges: { from: string; to: string; label?: string }[];
}

/** Where a node sits in the drawn graph, in pixels, and the layer it is in. */
export interface Placed {
  id: string;
  type: string;
  x: number;
  y: number;
  layer: number;
}

/** Lays the graph out in layers from its entry, top to bottom: each node one layer below the nearest node leading to it,
 * and a join below the deepest, so parallel branches stand side by side above it (#135). */
export function layered(graph: Graph): Placed[] {
  const next = new Map<string, string[]>();
  for (const e of graph.edges) next.set(e.from, [...(next.get(e.from) ?? []), e.to]);
  const layer = new Map<string, number>();
  const queue: string[] = [];
  if (graph.nodes.some((n) => n.id === graph.entry)) {
    layer.set(graph.entry, 0);
    queue.push(graph.entry);
  }
  while (queue.length > 0) {
    const id = queue.shift() as string;
    for (const to of next.get(id) ?? []) {
      if (!layer.has(to)) {
        layer.set(to, (layer.get(id) ?? 0) + 1);
        queue.push(to);
      }
    }
  }
  lowerJoins(graph, layer);
  // A node nothing leads to, which a valid workflow has not, still gets a place below the rest.
  const deepest = Math.max(-1, ...layer.values());
  const width = new Map<number, number>();
  return graph.nodes.map((n) => {
    const l = layer.get(n.id) ?? deepest + 1;
    const column = width.get(l) ?? 0;
    width.set(l, column + 1);
    return { id: n.id, type: n.type, layer: l, x: column * GRAPH_COLUMN_PX, y: l * GRAPH_ROW_PX };
  });
}

/** The edges, by index, that lead back to a node on the way to them, walking from the entry: each closes a loop (#134). */
export function loopEdges(graph: Graph): Set<number> {
  const out = new Map<string, number[]>();
  graph.edges.forEach((e, i) => out.set(e.from, [...(out.get(e.from) ?? []), i]));
  const walked = new Map<string, "open" | "closed">();
  const back = new Set<number>();
  const walk = (id: string) => {
    walked.set(id, "open");
    for (const i of out.get(id) ?? []) {
      const to = graph.edges[i]?.to ?? "";
      if (walked.get(to) === "open") back.add(i);
      else if (!walked.has(to)) walk(to);
    }
    walked.set(id, "closed");
  };
  const ids = graph.nodes.map((n) => n.id);
  for (const id of ids.includes(graph.entry) ? [graph.entry, ...ids] : ids) if (!walked.has(id)) walk(id);
  return back;
}

/** Moves each join below every branch into it, and what follows a moved node below it; loops are left out, so the
 * rest is acyclic and this ends within a round per node. */
function lowerJoins(graph: Graph, layer: Map<string, number>) {
  const loops = loopEdges(graph);
  const joins = new Set(graph.nodes.filter((n) => n.join).map((n) => n.id));
  const moved = new Set<string>();
  for (let round = 0, again = joins.size > 0; again && round <= graph.nodes.length; round++) {
    again = false;
    graph.edges.forEach((e, i) => {
      const from = layer.get(e.from);
      const to = layer.get(e.to);
      if (loops.has(i) || from === undefined || to === undefined || !(joins.has(e.to) || moved.has(e.from)) || to > from) return;
      layer.set(e.to, from + 1);
      moved.add(e.to);
      again = true;
    });
  }
}
