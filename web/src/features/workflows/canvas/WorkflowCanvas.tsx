import { useCallback, useMemo, useState } from "react";
import type { Problem } from "@/api/workflows";
import { EDITOR_GRAPH_HEIGHT_PX, MAX_BADGE_PROBLEMS } from "@/config";
import { GraphCanvas } from "@/features/graph/GraphCanvas";
import { s } from "@/strings";
import { AddNode } from "./AddNode";
import * as edit from "./edits";
import { Inspector, type NodeEdits } from "./Inspector";
import { edgeKey, placeProblems } from "./problems";
import { graphOf, type NodeType, nodePlace, OPTIONAL_FIELDS, parseSpec } from "./specDoc";
import { secondary } from "./styles";

const REGION = { "data-canvas": "" };
const NO_PROBLEMS: Problem[] = [];

/** The workflow drawn as a graph over its YAML: every change here is an edit of that text, which stays the source. */
export function WorkflowCanvas({
  text,
  onChange,
  problems,
  readOnly,
  focusAt,
}: {
  text: string;
  onChange: (text: string) => void;
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
      } catch {
        setFailed(true);
      }
    },
    [onChange, text],
  );

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
      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
        <GraphCanvas
          graph={graph}
          label={s.canvas.graph}
          height={EDITOR_GRAPH_HEIGHT_PX}
          draw={draw}
          edgeTone={edgeTone}
          selected={selected}
          onSelect={setChosen}
          region={REGION}
        />
        <Inspector
          spec={spec}
          selected={selected}
          onSelect={setChosen}
          problems={selected ? (placed.byNode.get(selected) ?? NO_PROBLEMS) : NO_PROBLEMS}
          readOnly={readOnly}
          edits={edits}
        />
      </div>
    </div>
  );
}
