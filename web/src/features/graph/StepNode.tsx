import { Handle, type Node, type NodeProps, Position } from "@xyflow/react";
import type { ReactNode } from "react";

/** What a drawn step carries: its label, and while the canvas edits, what its "+" does and says. */
export type StepData = { label: ReactNode; connectable: boolean; add?: { label: string; onAdd: () => void } };
export type StepNodeType = Node<StepData, "step">;

/** A step with a way in at the top and a way out at the bottom, as the layers run; edges need both even where nothing is
 * connected by hand. */
export function StepNode({ data, selected }: NodeProps<StepNodeType>) {
  const add = data.add;
  return (
    <>
      <Handle type="target" position={Position.Top} isConnectable={data.connectable} />
      {data.label}
      <Handle type="source" position={Position.Bottom} isConnectable={data.connectable} />
      {add && (
        <button
          type="button"
          aria-label={add.label}
          title={add.label}
          // On the node's corner, clear of the way out below and of the node under it; placed absolutely, it leaves
          // the node's measured size and so the layout as they are.
          className={`nodrag nopan absolute -right-3 -bottom-3 flex size-6 items-center justify-center rounded-full border border-border-strong bg-surface text-sm leading-none font-semibold text-ink shadow-1 hover:bg-accent hover:text-on-accent focus-visible:opacity-100 ${selected ? "opacity-100" : "opacity-0 group-hover:opacity-100"}`}
          onClick={(e) => {
            e.stopPropagation();
            add.onAdd();
          }}
          data-node-add
        >
          +
        </button>
      )}
    </>
  );
}

/** The node types the canvas draws; one object for good, since React Flow draws every node again for a new one. */
export const NODE_TYPES = { step: StepNode };
