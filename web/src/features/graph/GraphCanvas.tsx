import "@xyflow/react/dist/base.css";
import {
  type CoordinateExtent,
  type EdgeChange,
  type FinalConnectionState,
  type FitViewOptions,
  MarkerType,
  type NodeChange,
  ReactFlow,
  ReactFlowProvider,
  useNodesInitialized,
  useReactFlow,
} from "@xyflow/react";
import { type ReactNode, useEffect, useMemo, useRef } from "react";
import { GRAPH_COLUMN_PX, GRAPH_EXTENT_MARGIN_PX, GRAPH_FIT_MAX_ZOOM, GRAPH_LOOP_DASH, GRAPH_REVEAL_MS, GRAPH_REVEAL_PADDING, GRAPH_ROW_PX } from "@/config";
import { useCoarsePointer } from "@/lib/useMediaQuery";
import { s } from "@/strings";
import { type Graph, layered, loopEdges } from "./layout";
import { NODE_TYPES, type StepNodeType } from "./StepNode";
import { EDGE_TYPES, type WayEdgeType } from "./WayEdge";

/** How one node is drawn: what it says, its colours, and the attributes tests and readers find it by. */
export interface Drawn {
  label: ReactNode;
  tone: string;
  ariaLabel?: string;
  attributes?: Record<string, string>;
}

/** What the canvas does when it edits: each call is the page's to turn into a change of the text, which redraws it. */
export interface GraphEditing {
  canConnect: (from: string, to: string) => boolean;
  connect: (from: string, to: string) => void;
  /** Asks what to add after a node: from its "+", or a way dragged out of it into the open. */
  addAfter: (from: string) => void;
  insertOn: (edge: number) => void;
  remove: (what: { nodes: string[]; edges: number[] }) => void;
}

/** A node to bring into view once it is drawn; a new object each time, so adding the same id again still shows it. */
export type Reveal = { id: string };

const DELETE_KEYS = ["Delete", "Backspace"];
const FIT: FitViewOptions = { maxZoom: GRAPH_FIT_MAX_ZOOM };

/** A workflow's graph laid out in layers; the run page colours it by state, the editor selects, marks and edits it. */
export function GraphCanvas({
  graph,
  label,
  height,
  draw,
  edgeTone,
  selected,
  onSelect,
  region,
  editing,
  selectedEdge,
  onSelectEdge,
  reveal,
  toolbar,
}: {
  graph: Graph;
  label: string;
  /** A length in pixels, or any CSS length, such as one that follows the screen. */
  height: number | string;
  draw: (id: string, type: string) => Drawn;
  edgeTone?: (from: string, to: string) => string | undefined;
  selected?: string;
  onSelect?: (id: string) => void;
  region: Record<string, string>;
  editing?: GraphEditing;
  selectedEdge?: number;
  onSelectEdge?: (edge: number | undefined) => void;
  reveal?: Reveal;
  toolbar?: ReactNode;
}) {
  const selectable = onSelect !== undefined;
  // What React Flow measured of each node goes back with every redrawn node: a node redrawn before its measure
  // reached React would otherwise be left hidden, with its edges, as a run's events arrive in a burst.
  const measured = useRef(new Map<string, { width: number; height: number }>());
  const coarse = useCoarsePointer();
  const placed = useMemo(() => layered(graph), [graph]);
  const extent = useMemo(() => reach(placed), [placed]);
  const nodes = useMemo<StepNodeType[]>(
    () =>
      placed.map((p) => {
        const drawn = draw(p.id, p.type);
        return {
          id: p.id,
          type: "step",
          position: { x: p.x, y: p.y },
          measured: measured.current.get(p.id),
          data: {
            label: drawn.label,
            connectable: editing !== undefined,
            add: editing ? { label: s.canvas.graphEdit.addAfter(p.id), onAdd: () => editing.addAfter(p.id) } : undefined,
          },
          ariaLabel: drawn.ariaLabel,
          className: `group rounded-control border-2 px-3 py-2 text-sm ${drawn.tone} ${p.id === selected ? "outline-2 outline-offset-2 outline-focus" : ""}`,
          domAttributes: { "data-node": p.id, "data-layer": String(p.layer), ...drawn.attributes },
          draggable: false,
          connectable: editing !== undefined,
          selectable,
          selected: p.id === selected,
          // React Flow passes data- attributes through to the node, though its type leaves them out.
        } as StepNodeType;
      }),
    [placed, draw, selected, selectable, editing],
  );
  const edges = useMemo<WayEdgeType[]>(() => {
    const loops = loopEdges(graph);
    return graph.edges.map((e, i) => {
      const stroke = edgeTone?.(e.from, e.to) ?? "var(--color-border-strong)";
      const loop = loops.has(i);
      const tools = editing && {
        insert: () => editing.insertOn(i),
        remove: () => editing.remove({ nodes: [], edges: [i] }),
        insertLabel: s.canvas.graphEdit.insertOn(e.from, e.to),
        removeLabel: s.canvas.graphEdit.removeWay(e.from, e.to),
      };
      // An edge's id is its place in the graph, so a change of the selection or a removal leads straight back to it.
      const edge: WayEdgeType = {
        id: String(i),
        type: "way",
        source: e.from,
        target: e.to,
        label: e.label,
        className: "text-ink-muted",
        labelStyle: { fill: "var(--color-ink-muted)" },
        labelBgStyle: { fill: "var(--color-surface)" },
        style: loop ? { stroke, strokeDasharray: GRAPH_LOOP_DASH } : { stroke },
        data: { loop, tools: tools || undefined },
        ariaLabel: editing ? s.canvas.graphEdit.way(e.from, e.to, e.label) : undefined,
        focusable: editing !== undefined,
        selectable: editing !== undefined,
        selected: editing !== undefined && i === selectedEdge,
      };
      if (!loop) return edge;
      const attributes: Record<string, string> = { "data-edge-loop": "", "data-from": e.from, "data-to": e.to };
      return { ...edge, markerEnd: { type: MarkerType.ArrowClosed, color: stroke }, domAttributes: attributes } as WayEdgeType;
    });
  }, [graph, edgeTone, editing, selectedEdge]);

  // Selection is the page's to keep: a click or Enter on a focused node reaches it as a select change.
  function changed(changes: NodeChange<StepNodeType>[]) {
    for (const c of changes) {
      if (c.type === "dimensions" && c.dimensions) measured.current.set(c.id, c.dimensions);
      if (c.type === "select" && c.selected) onSelect?.(c.id);
    }
  }
  function edgesChanged(changes: EdgeChange<WayEdgeType>[]) {
    for (const c of changes) {
      if (c.type !== "select") continue;
      if (c.selected) onSelectEdge?.(Number(c.id));
      else if (Number(c.id) === selectedEdge) onSelectEdge?.(undefined);
    }
  }
  // A way dragged out of a node and let go in the open asks what to add there.
  function connectEnd(_: MouseEvent | TouchEvent, state: FinalConnectionState) {
    if (state.fromNode && !state.toNode && state.fromHandle?.type === "source") editing?.addAfter(state.fromNode.id);
  }

  // The toolbar sits above the graph, not over it, so it never covers a node, however it wraps on a narrow screen.
  return (
    <section aria-label={label} className="flex flex-col overflow-hidden rounded-overlay border border-border bg-surface-sunken" style={{ height }} {...region}>
      <ReactFlowProvider>
        {toolbar && <div className="border-b border-border p-2">{toolbar}</div>}
        <div className="min-h-0 flex-1">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={NODE_TYPES}
            edgeTypes={EDGE_TYPES}
            onNodesChange={changed}
            // A click on the node already selected changes nothing in React Flow, yet still chooses it for the page.
            onNodeClick={selectable ? (_, node) => onSelect?.(node.id) : undefined}
            onEdgesChange={edgesChanged}
            fitView
            fitViewOptions={FIT}
            translateExtent={extent}
            nodesDraggable={false}
            nodesConnectable={editing !== undefined}
            edgesFocusable={editing !== undefined}
            elementsSelectable={selectable}
            connectOnClick={false}
            isValidConnection={(c) => editing?.canConnect(c.source, c.target) ?? false}
            onConnect={(c) => editing?.connect(c.source, c.target)}
            onConnectEnd={editing ? connectEnd : undefined}
            // React Flow never removes anything itself: the page changes the text, and the graph is drawn again from it.
            deleteKeyCode={editing ? DELETE_KEYS : null}
            onBeforeDelete={async (d) => {
              editing?.remove({ nodes: d.nodes.map((n) => n.id), edges: d.edges.map((e) => Number(e.id)) });
              return false;
            }}
            // A wheel scrolls the page past the graph; a pinch, or Ctrl and the wheel, zooms it. A finger scrolls the page
            // too, so on a touch screen the graph moves by the toolbar's zoom and fit, and to a node focused or added.
            panOnDrag={!coarse}
            zoomOnScroll={false}
            zoomOnPinch
            preventScrolling={false}
          >
            {reveal && <Revealer reveal={reveal} />}
          </ReactFlow>
        </div>
      </ReactFlowProvider>
    </section>
  );
}

/** How far the graph can be moved: round its nodes with room to spare, so a fling never loses it; anywhere when empty. */
function reach(placed: { x: number; y: number }[]): CoordinateExtent | undefined {
  if (placed.length === 0) return undefined;
  const xs = placed.map((p) => p.x);
  const ys = placed.map((p) => p.y);
  return [
    [Math.min(...xs) - GRAPH_EXTENT_MARGIN_PX, Math.min(...ys) - GRAPH_EXTENT_MARGIN_PX],
    [Math.max(...xs) + GRAPH_COLUMN_PX + GRAPH_EXTENT_MARGIN_PX, Math.max(...ys) + GRAPH_ROW_PX + GRAPH_EXTENT_MARGIN_PX],
  ];
}

/** Brings a new node into view once React Flow has measured it, at the zoom the person chose. */
function Revealer({ reveal }: { reveal: Reveal }) {
  const flow = useReactFlow();
  const ready = useNodesInitialized();
  const shown = useRef<Reveal | undefined>(undefined);
  useEffect(() => {
    if (!ready || shown.current === reveal || !flow.getNode(reveal.id)) return;
    shown.current = reveal;
    void flow.fitView({ nodes: [{ id: reveal.id }], maxZoom: flow.getZoom(), padding: GRAPH_REVEAL_PADDING, duration: GRAPH_REVEAL_MS });
  }, [flow, ready, reveal]);
  return null;
}
