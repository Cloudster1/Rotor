import { type Edge, type Node, ReactFlow } from "@xyflow/react";
import "@xyflow/react/dist/base.css";
import { useMemo } from "react";
import type { RunGraph as Graph } from "@/api/runs";
import { GRAPH_HEIGHT_PX } from "@/config";
import { s } from "@/strings";
import { layered } from "./layout";
import type { NodeState } from "./timeline";

/** How each state reads on a node: settled ones in their colour, a running one in the accent. */
const tone: Record<NodeState, string> = {
  pending: "border-border bg-surface text-ink-muted",
  running: "border-accent bg-accent-subtle text-ink",
  done: "border-success bg-success-subtle text-ink",
  failed: "border-danger bg-danger-subtle text-ink",
  waiting: "border-warning bg-warning-subtle text-ink",
};

/** The run's workflow as a graph, each node coloured by where the run is; the timeline beside it says the same in words. */
export function RunGraph({ graph, states }: { graph: Graph; states: Record<string, NodeState> }) {
  const nodes = useMemo<Node[]>(
    () =>
      layered(graph).map((p) => {
        const state = states[p.id] ?? "pending";
        return {
          id: p.id,
          position: { x: p.x, y: p.y },
          data: {
            label: (
              <span className="flex flex-col items-center gap-0.5">
                <span className="font-medium">{p.id}</span>
                <span className="text-xs">
                  {p.type} . {s.runs.live.nodeState[state]}
                </span>
              </span>
            ),
          },
          className: `rounded-control border-2 px-3 py-2 text-sm ${tone[state]}`,
          domAttributes: { "data-node": p.id, "data-node-state": state },
          draggable: false,
          connectable: false,
          selectable: false,
        } as Node;
      }),
    [graph, states],
  );
  const edges = useMemo<Edge[]>(
    () =>
      graph.edges.map((e, i) => ({
        id: `${e.from}-${e.to}-${i}`,
        source: e.from,
        target: e.to,
        label: e.label,
        className: "text-ink-muted",
        labelStyle: { fill: "var(--color-ink-muted)" },
        labelBgStyle: { fill: "var(--color-surface)" },
        style: { stroke: "var(--color-border-strong)" },
      })),
    [graph],
  );
  return (
    <section
      aria-label={s.runs.live.graph}
      className="rounded-overlay border border-border bg-surface-sunken"
      style={{ height: GRAPH_HEIGHT_PX }}
      data-run-graph
    >
      <ReactFlow
        nodes={nodes}
        edges={edges}
        fitView
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        panOnDrag
        zoomOnScroll={false}
      />
    </section>
  );
}
