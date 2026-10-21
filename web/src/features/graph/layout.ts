import { GRAPH_COLUMN_PX, GRAPH_ROW_PX } from "@/config";

/** A workflow's nodes and the ways between them, entry first, as the server's Spec.Graph() draws it. */
export interface Graph {
  entry: string;
  nodes: { id: string; type: string }[];
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

/** Lays the graph out in layers from its entry, top to bottom: each node one layer below the nearest node leading to it. */
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
