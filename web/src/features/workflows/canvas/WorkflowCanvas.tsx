import { useCallback, useMemo, useState } from "react";
import type { Problem } from "@/api/workflows";
import { EDITOR_GRAPH_HEIGHT_PX, MAX_BADGE_PROBLEMS } from "@/config";
import { GraphCanvas } from "@/features/graph/GraphCanvas";
import type { Graph } from "@/features/graph/layout";
import type { TextHistory } from "@/features/workflows/history";
import { s } from "@/strings";
import { AddNode } from "./AddNode";
import * as edit from "./edits";
import { Inspector, type NodeEdits } from "./Inspector";
import { edgeKey, placeProblems } from "./problems";
import { graphOf, type NodeType, nodePlace, OPTIONAL_FIELDS, parseSpec, type SpecShape } from "./specDoc";
import { secondary } from "./styles";
import { useGraphEditing } from "./useGraphEditing";

const REGION = { "data-canvas": "" };
const NO_PROBLEMS: Problem[] = [];
const NO_SPEC: SpecShape = { entry: "", nodes: [], edges: [], bindings: [] };
const NO_GRAPH: Graph = { entry: "", nodes: [], edges: [] };

/** The workflow drawn as a graph over its YAML: every change here is an edit of that text, which stays the source. */
export function WorkflowCanvas({
  text,
  onChange,
  history,
  problems,
  readOnly,
  focusAt,
}: {
  text: string;
  onChange: (text: string) => void;
  history: TextHistory;
  problems: Problem[];
  readOnly: boolean;
  focusAt: (line: number, column: number) => void;
}) {
  const parsed = useMemo(() => parseSpec(text), [text]);
  const spec = parsed?.spec;
  const [chosen, setChosen] = useState<string | undefined>(undefined);
  const [failed, setFailed] = useState(false);
  const ids = spec?.nodes.map((n) => n.id) ?? [];
  const selected = chosen && ids.includes(chosen) ? chosen : (spec?.nodes.find((n) => n.id === spec.entry)?.id ?? ids[0]);
  const placed = useMemo(() => (spec ? placeProblems(spec, problems) : undefined), [spec, problems]);
  const graph = useMemo(() => (spec ? graphOf(spec) : undefined), [spec]);

  const draw = useCallback(
    (id: string, type: string) => {
      const found = placed?.byNode.get(id) ?? NO_PROBLEMS;
      const kind = s.canvas.types[type as NodeType] ?? type;
      const entry = spec?.entry === id;
      const count = found.length > MAX_BADGE_PROBLEMS ? s.canvas.node.many(MAX_BADGE_PROBLEMS) : s.canvas.node.problems(found.length);
      return {
        label: (
          <span className="flex flex-col items-center gap-0.5">
            <span className="font-medium">{id}</span>
            <span className="text-xs">
              {kind}
              {entry ? ` . ${s.canvas.node.entry}` : ""}
            </span>
            {found.length > 0 && (
              <span className="rounded-full bg-danger px-2 text-xs font-medium text-on-danger" data-node-badge>
                {count}
              </span>
            )}
          </span>
        ),
        tone: found.length > 0 ? "border-danger bg-danger-subtle text-ink" : "border-border-strong bg-surface text-ink",
        ariaLabel: [id, kind, entry ? s.canvas.node.entry : "", found.length > 0 ? count : ""].filter(Boolean).join(", "),
        attributes: { "data-node-problems": String(found.length) },
      };
    },
    [placed, spec?.entry],
  );
  const edgeTone = useCallback((from: string, to: string) => (placed?.edges.has(edgeKey(from, to)) ? "var(--color-danger)" : undefined), [placed]);

  // An edit that cannot hold leaves the text as it was and says so; the YAML can always make the change.
  const apply = useCallback(
    (change: (source: string) => string) => {
      try {
        onChange(change(text));
        setFailed(false);
        return true;
      } catch {
        setFailed(true);
        return false;
      }
    },
    [onChange, text],
  );
  const graphEdit = useGraphEditing({ text, spec: spec ?? NO_SPEC, graph: graph ?? NO_GRAPH, apply, selected, select: setChosen, history });

  if (!spec || !graph || !placed) {
    return (
      <div className="flex flex-col items-start gap-3 rounded-overlay border border-border bg-surface-raised p-4" data-canvas-unparsed>
        <p className="text-sm text-ink">{s.canvas.unparsed}</p>
        <button type="button" className={secondary} onClick={() => focusAt(1, 1)}>
          {s.canvas.openYaml}
        </button>
      </div>
    );
  }

  const node = selected ?? "";
  const edits: NodeEdits = {
    rename: (to) => {
      apply((t) => edit.renameNode(t, node, to));
      setChosen(to);
    },
    setEntry: () => apply((t) => edit.setEntry(t, node)),
    setField: (key, value) =>
      apply((t) => (value === "" && OPTIONAL_FIELDS.includes(key) ? edit.clearField(t, node, key) : edit.setField(t, node, key, value))),
    setModel: (model) => apply((t) => edit.setModel(t, node, model)),
    remove: () => {
      apply((t) => edit.removeNode(t, node));
      setChosen(undefined);
    },
    openYaml: () => {
      const at = nodePlace(text, node);
      focusAt(at?.line ?? 1, at?.column ?? 1);
    },
    focusAt,
    addEdge: (to) => apply((t) => edit.addEdge(t, node, to)),
    removeEdge: (to) => apply((t) => edit.removeEdge(t, node, to)),
    setCase: (value, to) => apply((t) => edit.setCase(t, node, value, to)),
    removeCase: (value) => apply((t) => edit.removeCase(t, node, value)),
    setDefault: (to) => apply((t) => edit.setDefault(t, node, to)),
  };

  return (
    <div className="flex flex-col gap-4">
      {!readOnly && (
        <AddNode
          taken={ids}
          onAdd={(type, id) => {
            apply((t) => edit.addNode(t, type, id));
            setChosen(id);
          }}
        />
      )}
      {failed && (
        <p role="alert" className="rounded-control border border-danger bg-danger-subtle px-3 py-2 text-sm text-danger" data-canvas-failed>
          {s.canvas.failed}
        </p>
      )}
      <p className="sr-only" aria-live="polite" data-canvas-said>
        {graphEdit.said}
      </p>
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
        {/* The canvas's keys, undo, redo and add, reach it from the graph and its toolbar, wherever the focus is in them. */}
        <div onKeyDown={readOnly ? undefined : graphEdit.onKeyDown}>
          <GraphCanvas
            graph={graph}
            label={s.canvas.graph}
            height={EDITOR_GRAPH_HEIGHT_PX}
            draw={draw}
            edgeTone={edgeTone}
            // While a way is selected no node is, so Delete takes only the way.
            selected={graphEdit.selectedEdge === undefined ? selected : undefined}
            onSelect={(id) => {
              setChosen(id);
              graphEdit.selectEdge(undefined);
            }}
            region={REGION}
            editing={readOnly ? undefined : graphEdit.editing}
            selectedEdge={graphEdit.selectedEdge}
            onSelectEdge={graphEdit.selectEdge}
            reveal={graphEdit.reveal}
            toolbar={readOnly ? undefined : graphEdit.toolbar}
          />
        </div>
        <Inspector
          spec={spec}
          selected={selected}
          onSelect={setChosen}
          problems={selected ? (placed.byNode.get(selected) ?? NO_PROBLEMS) : NO_PROBLEMS}
          readOnly={readOnly}
          edits={edits}
        />
      </div>
      {!readOnly && graphEdit.dialog}
    </div>
  );
}
