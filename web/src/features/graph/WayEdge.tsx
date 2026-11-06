import { BaseEdge, type Edge, EdgeLabelRenderer, type EdgeProps, getBezierPath, getSmoothStepPath } from "@xyflow/react";
import { GRAPH_LOOP_OFFSET_PX } from "@/config";

/** What a drawn way carries: whether it loops back, and while the canvas edits, what its buttons do and say. */
export type WayData = {
  loop: boolean;
  tools?: { insert: () => void; remove: () => void; insertLabel: string; removeLabel: string };
};
export type WayEdgeType = Edge<WayData, "way">;

const tool =
  "flex size-6 items-center justify-center rounded-full border border-border-strong bg-surface text-sm leading-none font-semibold text-ink shadow-1 hover:bg-surface-raised";

/** A way between two steps; a way back swings out round its nodes rather than cutting up through them (#134). */
export function WayEdge(props: EdgeProps<WayEdgeType>) {
  const { id, data, selected, label, labelStyle, labelBgStyle, style, markerEnd } = props;
  const at = {
    sourceX: props.sourceX,
    sourceY: props.sourceY,
    targetX: props.targetX,
    targetY: props.targetY,
    sourcePosition: props.sourcePosition,
    targetPosition: props.targetPosition,
  };
  const [path, labelX, labelY] = data?.loop ? getSmoothStepPath({ ...at, offset: GRAPH_LOOP_OFFSET_PX }) : getBezierPath(at);
  const tools = selected ? data?.tools : undefined;
  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        label={label}
        labelX={labelX}
        labelY={labelY}
        labelStyle={labelStyle}
        labelBgStyle={labelBgStyle}
        style={style}
        markerEnd={markerEnd}
      />
      {tools && (
        <EdgeLabelRenderer>
          <div
            className="nodrag nopan pointer-events-auto absolute flex gap-1"
            // Below the way's label, so both can be read while it is selected.
            style={{ transform: `translate(-50%, 0.75rem) translate(${labelX}px, ${labelY}px)` }}
            data-edge-tools
          >
            <button type="button" className={tool} aria-label={tools.insertLabel} title={tools.insertLabel} onClick={tools.insert} data-edge-insert>
              +
            </button>
            <button type="button" className={tool} aria-label={tools.removeLabel} title={tools.removeLabel} onClick={tools.remove} data-edge-remove>
              x
            </button>
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

/** The edge types the canvas draws, one object for good, as the node types are. */
export const EDGE_TYPES = { way: WayEdge };
