import { type Edge, MarkerType, type Node, type NodeChange, ReactFlow } from "@xyflow/react";
import "@xyflow/react/dist/base.css";
import { type ReactNode, useMemo } from "react";
import { GRAPH_LOOP_DASH, GRAPH_LOOP_OFFSET_PX } from "@/config";
import { type Graph, layered, loopEdges } from "./layout";

/** How one node is drawn: what it says, its colours, and the attributes tests and readers find it by. */
export interface Drawn {
  label: ReactNode;
  tone: string;
  ariaLabel?: string;
  attributes?: Record<string, string>;
}

/** A workflow's graph laid out in layers; the run page colours it by state, the editor selects and marks it. */
export function GraphCanvas({
  graph,
  label,
  height,
  draw,
  edgeTone,
  selected,
  onSelect,
  region,
}: {
  graph: Graph;
  label: string;
  height: number;
  draw: (id: string, type: string) => Drawn;
  edgeTone?: (from: string, to: string) => string | undefined;
  selected?: string;
  onSelect?: (id: string) => void;
  region: Record<string, string>;
}) {
  const selectable = onSelect !== undefined;
  const nodes = useMemo<Node[]>(
    () =>
      layered(graph).map((p) => {
        const drawn = draw(p.id, p.type);
        return {
          id: p.id,
          position: { x: p.x, y: p.y },
          data: { label: drawn.label },
          ariaLabel: drawn.ariaLabel,
          className: `rounded-control border-2 px-3 py-2 text-sm ${drawn.tone} ${p.id === selected ? "outline-2 outline-offset-2 outline-focus" : ""}`,
          domAttributes: { "data-node": p.id, "data-layer": String(p.layer), ...drawn.attributes },
          draggable: false,
          connectable: false,
          selectable,
          selected: p.id === selected,
        } as Node;
      }),
    [graph, draw, selected, selectable],
  );
  const edges = useMemo<Edge[]>(() => {
    const loops = loopEdges(graph);
    return graph.edges.map((e, i) => {
      const stroke = edgeTone?.(e.from, e.to) ?? "var(--color-border-strong)";
      const edge: Edge = {
        id: `${e.from}-${e.to}-${i}`,
        source: e.from,
        target: e.to,
        label: e.label,
        className: "text-ink-muted",
        labelStyle: { fill: "var(--color-ink-muted)" },
        labelBgStyle: { fill: "var(--color-surface)" },
        style: { stroke },
        focusable: false,
        selectable: false,
      };
      if (!loops.has(i)) return edge;
      // A way back swings out around its nodes, dashed and pointed, rather than cutting up through them.
      const attributes: Record<string, string> = { "data-edge-loop": "", "data-from": e.from, "data-to": e.to };
      return {
        ...edge,
        type: "smoothstep",
        pathOptions: { offset: GRAPH_LOOP_OFFSET_PX },
        style: { stroke, strokeDasharray: GRAPH_LOOP_DASH },
        markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
        domAttributes: attributes,
      } as Edge;
    });
  }, [graph, edgeTone]);

  // Selection is the page's to keep: a click or Enter on a focused node reaches it as a select change.
  function changed(changes: NodeChange[]) {
    for (const c of changes) if (c.type === "select" && c.selected) onSelect?.(c.id);
  }

  return (
    <section aria-label={label} className="rounded-overlay border border-border bg-surface-sunken" style={{ height }} {...region}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={selectable ? changed : undefined}
        fitView
        nodesDraggable={false}
        nodesConnectable={false}
        edgesFocusable={false}
        elementsSelectable={selectable}
        panOnDrag
        zoomOnScroll={false}
      />
    </section>
  );
}
