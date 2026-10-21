import { useCallback } from "react";
import type { RunGraph as Graph } from "@/api/runs";
import { GRAPH_HEIGHT_PX } from "@/config";
import { GraphCanvas } from "@/features/graph/GraphCanvas";
import { s } from "@/strings";
import type { NodeState } from "./timeline";

/** How each state reads on a node: settled ones in their colour, a running one in the accent. */
const tone: Record<NodeState, string> = {
  pending: "border-border bg-surface text-ink-muted",
  running: "border-accent bg-accent-subtle text-ink",
  done: "border-success bg-success-subtle text-ink",
  failed: "border-danger bg-danger-subtle text-ink",
  waiting: "border-warning bg-warning-subtle text-ink",
};

const REGION = { "data-run-graph": "" };

/** The run's workflow as a graph, each node coloured by where the run is; the timeline beside it says the same in words. */
export function RunGraph({ graph, states }: { graph: Graph; states: Record<string, NodeState> }) {
  const draw = useCallback(
    (id: string, type: string) => {
      const state = states[id] ?? "pending";
      return {
        label: (
          <span className="flex flex-col items-center gap-0.5">
            <span className="font-medium">{id}</span>
            <span className="text-xs">
              {type} . {s.runs.live.nodeState[state]}
            </span>
          </span>
        ),
        tone: tone[state],
        attributes: { "data-node-state": state },
      };
    },
    [states],
  );
  return <GraphCanvas graph={graph} label={s.runs.live.graph} height={GRAPH_HEIGHT_PX} draw={draw} region={REGION} />;
}
