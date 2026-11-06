import { type KeyboardEvent, type ReactNode, useState } from "react";
import type { GraphEditing, Reveal } from "@/features/graph/GraphCanvas";
import type { Graph } from "@/features/graph/layout";
import type { TextHistory } from "@/features/workflows/history";
import { s } from "@/strings";
import { CanvasToolbar } from "./CanvasToolbar";
import { CaseDialog } from "./CaseDialog";
import { NodePicker } from "./NodePicker";
import type { NodeType, SpecShape } from "./specDoc";
import * as ways from "./ways";

/** What the canvas is asking about: which node to add after one, or on a way, or how a router goes to a node. */
type Pending = { kind: "add"; from?: string } | { kind: "insert"; edge: number } | { kind: "case"; from: string; to: string };

const TYPING = "input, textarea, select, [contenteditable='true'], dialog";

/** Everything the canvas edits on its graph: connecting, adding and inserting nodes, removing, and the keys for it all.
 * Each change goes through apply, which edits the YAML and says whether that held. */
export function useGraphEditing({
  text,
  spec,
  graph,
  apply,
  selected,
  select,
  history,
}: {
  text: string;
  spec: SpecShape;
  graph: Graph;
  apply: (change: (source: string) => string) => boolean;
  selected?: string;
  select: (id: string | undefined) => void;
  history: TextHistory;
}) {
  const [pending, setPending] = useState<Pending>();
  const [reveal, setReveal] = useState<Reveal>();
  // A way is selected by its place in the graph of one text; any change of the text lets go of it.
  const [edgeAt, setEdgeAt] = useState<{ text: string; edge: number }>();
  const [said, setSaid] = useState("");
  const selectedEdge = edgeAt?.text === text && graph.edges[edgeAt.edge] ? edgeAt.edge : undefined;
  const ids = spec.nodes.map((n) => n.id);
  const routerOf = (id: string | undefined) => spec.nodes.find((n) => n.id === id && n.type === "router");

  function remove(what: { nodes: string[]; edges: number[] }) {
    if (what.nodes.length === 0 && what.edges.length === 0) return;
    if (!apply((t) => ways.removeAll(t, graph, what))) return;
    setSaid(s.canvas.graphEdit.removed);
    setEdgeAt(undefined);
    if (selected && what.nodes.includes(selected)) select(undefined);
  }

  const editing: GraphEditing = {
    canConnect: (from, to) => ways.canConnect(spec, from, to),
    connect: (from, to) => {
      // A router goes on by a case or its default, never an edge, so the person says which.
      if (routerOf(from)) setPending({ kind: "case", from, to });
      else if (apply((t) => ways.connect(t, from, to, { kind: "edge" }))) setSaid(s.canvas.graphEdit.connected(from, to));
    },
    addAfter: (from) => setPending({ kind: "add", from }),
    insertOn: (edge) => setPending({ kind: "insert", edge }),
    remove,
  };

  function added(id: string, held: boolean) {
    if (!held) return;
    select(id);
    setEdgeAt(undefined);
    setReveal({ id });
    setSaid(s.canvas.graphEdit.added(id));
  }

  function pick(type: NodeType, caseValue?: string) {
    const asked = pending;
    setPending(undefined);
    const id = ways.freshId(type, ids);
    if (asked?.kind === "add") {
      const from = asked.from && ids.includes(asked.from) ? asked.from : undefined;
      const out = from ? ways.wayOut(spec, from) : undefined;
      const way: ways.Way | undefined = out === "case" ? { kind: "case", value: caseValue ?? "" } : out;
      added(
        id,
        apply((t) => ways.addAfter(t, type, id, from && way ? { id: from, way } : undefined)),
      );
    } else if (asked?.kind === "insert") {
      const e = graph.edges[asked.edge];
      if (e)
        added(
          id,
          apply((t) => ways.insertBetween(t, type, id, e.from, e.to, ways.wayOf(e))),
        );
    }
  }

  function connectRouter(from: string, to: string, way: ways.Way) {
    setPending(undefined);
    if (apply((t) => ways.connect(t, from, to, way))) setSaid(s.canvas.graphEdit.connected(from, to));
  }

  const addHere = () => setPending({ kind: "add", from: selected && ids.includes(selected) ? selected : undefined });
  const close = () => setPending(undefined);

  let dialog: ReactNode = null;
  if (pending?.kind === "add") {
    const router = routerOf(pending.from);
    dialog = (
      <NodePicker
        title={pending.from ? s.canvas.picker.after(pending.from) : s.canvas.picker.add}
        caseOf={router?.default ? { router: router.id, taken: router.cases.map(([v]) => v) } : undefined}
        onPick={pick}
        onClose={close}
      />
    );
  } else if (pending?.kind === "insert") {
    const e = graph.edges[pending.edge];
    if (e) dialog = <NodePicker title={s.canvas.picker.between(e.from, e.to)} onPick={pick} onClose={close} />;
  } else if (pending?.kind === "case") {
    const { from, to } = pending;
    const router = routerOf(from);
    dialog = (
      <CaseDialog
        from={from}
        to={to}
        current={router?.default}
        taken={router?.cases.map(([v]) => v) ?? []}
        onCase={(value) => connectRouter(from, to, { kind: "case", value })}
        onDefault={() => connectRouter(from, to, { kind: "default" })}
        onClose={close}
      />
    );
  }

  function undo() {
    if (!history.canUndo) return;
    history.undo();
    setSaid(s.canvas.graphEdit.undone);
  }
  function redo() {
    if (!history.canRedo) return;
    history.redo();
    setSaid(s.canvas.graphEdit.redone);
  }

  // The keys work while the graph holds the focus, and never while something is typed, so a single "a" is safe.
  function onKeyDown(event: KeyboardEvent) {
    if ((event.target as HTMLElement).closest(TYPING)) return;
    const mod = event.ctrlKey || event.metaKey;
    const key = event.key.toLowerCase();
    if (mod && key === "z" && !event.shiftKey) undo();
    else if (mod && ((key === "z" && event.shiftKey) || key === "y")) redo();
    else if (!mod && !event.altKey && key === "a") addHere();
    else return;
    event.preventDefault();
  }

  const edge = selectedEdge;
  const toolbar = (
    <CanvasToolbar
      onAdd={addHere}
      onInsert={edge === undefined ? undefined : () => setPending({ kind: "insert", edge })}
      onRemove={edge !== undefined ? () => remove({ nodes: [], edges: [edge] }) : selected ? () => remove({ nodes: [selected], edges: [] }) : undefined}
      history={{ ...history, undo, redo }}
    />
  );

  return {
    editing,
    reveal,
    selectedEdge,
    selectEdge: (i: number | undefined) => setEdgeAt(i === undefined ? undefined : { text, edge: i }),
    toolbar,
    dialog,
    said,
    onKeyDown,
  };
}
