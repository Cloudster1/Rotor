import type { Problem } from "@/api/workflows";
import type { SpecShape } from "./specDoc";

/** The checks' findings placed on the canvas: on the node they are about, on an edge's starting node, or nowhere. */
export interface Placed {
  byNode: Map<string, Problem[]>;
  edges: Set<string>;
  elsewhere: Problem[];
}

/** The key an edge is marked under, from its two ends. */
export const edgeKey = (from: string, to: string) => `${from}\n${to}`;

/** A JSON pointer's segments, unescaped as RFC 6901 says. */
function segments(pointer: string): string[] {
  return pointer === ""
    ? []
    : pointer
        .slice(1)
        .split("/")
        .map((s) => s.replace(/~1/g, "/").replace(/~0/g, "~"));
}

const indexIn = (segment: string | undefined, length: number) => {
  const i = /^\d+$/.test(segment ?? "") ? Number(segment) : -1;
  return i < length ? i : -1;
};

/** Places each finding by its path: /nodes/{i}... on node i, /edges/{i}... on the edge and its starting node. */
export function placeProblems(spec: SpecShape, problems: Problem[]): Placed {
  const placed: Placed = { byNode: new Map(), edges: new Set(), elsewhere: [] };
  const ids = new Set(spec.nodes.map((n) => n.id).filter((id) => id !== ""));
  const on = (id: string, p: Problem) => placed.byNode.set(id, [...(placed.byNode.get(id) ?? []), p]);
  for (const p of problems) {
    const [kind, at] = segments(p.path);
    if (kind === "nodes") {
      const node = spec.nodes[indexIn(at, spec.nodes.length)];
      if (node && ids.has(node.id)) {
        on(node.id, p);
        continue;
      }
    }
    if (kind === "edges") {
      const edge = spec.edges[indexIn(at, spec.edges.length)];
      if (edge) {
        placed.edges.add(edgeKey(edge.from, edge.to));
        if (ids.has(edge.from)) {
          on(edge.from, p);
          continue;
        }
      }
    }
    placed.elsewhere.push(p);
  }
  return placed;
}
