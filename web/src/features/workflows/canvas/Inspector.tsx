import { type FormEvent, useId, useState } from "react";
import type { Problem } from "@/api/workflows";
import { s } from "@/strings";
import { NodeFields } from "./NodeFields";
import { NodeWays, type WayEdits } from "./NodeWays";
import { type ModelRef, NODE_ID, type NodeType, type SpecShape } from "./specDoc";
import { danger, input, label, secondary } from "./styles";

/** What the inspector asks of the canvas, each turned into a YAML edit or a move to the YAML. */
export interface NodeEdits extends WayEdits {
  rename: (to: string) => void;
  setEntry: () => void;
  setField: (key: string, value: string) => void;
  setModel: (model?: ModelRef) => void;
  remove: () => void;
  openYaml: () => void;
  focusAt: (line: number, column: number) => void;
}

/** The selected node, editable without a pointer: which node, its id, main fields, ways out, and its problems. */
export function Inspector({
  spec,
  selected,
  onSelect,
  problems,
  readOnly,
  edits,
}: {
  spec: SpecShape;
  selected: string | undefined;
  onSelect: (id: string) => void;
  problems: Problem[];
  readOnly: boolean;
  edits: NodeEdits;
}) {
  const id = useId();
  const node = spec.nodes.find((n) => n.id === selected && n.id !== "");
  const ids = spec.nodes.map((n) => n.id).filter((n) => n !== "");
  return (
    <section className="flex flex-col gap-4 rounded-overlay border border-border bg-surface p-4" aria-labelledby={`${id}-title`} data-inspector>
      <h3 id={`${id}-title`} className="text-sm font-semibold text-ink">
        {s.canvas.node.title}
      </h3>
      <label className={label}>
        {s.canvas.node.choose}
        <select className={input} value={node?.id ?? ""} onChange={(e) => onSelect(e.target.value)} name="selected-node">
          {!node && <option value="">{s.canvas.node.chooseNode}</option>}
          {ids.map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
        </select>
      </label>
      {!node ? (
        <p className="text-sm text-ink-muted">{s.canvas.node.none}</p>
      ) : (
        <Selected key={node.id} nodeID={node.id} spec={spec} problems={problems} readOnly={readOnly} edits={edits} />
      )}
    </section>
  );
}

function Selected({ nodeID, spec, problems, readOnly, edits }: { nodeID: string; spec: SpecShape; problems: Problem[]; readOnly: boolean; edits: NodeEdits }) {
  const id = useId();
  const node = spec.nodes.find((n) => n.id === nodeID);
  const [name, setName] = useState(nodeID);
  const [error, setError] = useState("");
  const [removing, setRemoving] = useState(false);
  if (!node) return null;

  function rename(event: FormEvent) {
    event.preventDefault();
    const wanted = name.trim();
    if (wanted === nodeID) return;
    if (!NODE_ID.test(wanted)) return setError(s.canvas.add.badID);
    if (spec.nodes.some((n) => n.id === wanted)) return setError(s.canvas.add.taken(wanted));
    setError("");
    edits.rename(wanted);
  }

  return (
    <div className="flex flex-col gap-4" data-selected-node={nodeID}>
      <p className="text-sm text-ink-muted">{s.canvas.node.type(s.canvas.types[node.type as NodeType] ?? node.type)}</p>
      {!readOnly && (
        <form className="flex items-end gap-2" onSubmit={rename}>
          <label className={`${label} flex-1`}>
            {s.canvas.node.id}
            <input
              className={`${input} font-mono`}
              value={name}
              onChange={(e) => setName(e.target.value)}
              name="node-rename"
              aria-invalid={error !== ""}
              aria-describedby={error ? `${id}-error` : undefined}
            />
          </label>
          <button type="submit" className={secondary}>
            {s.canvas.node.rename}
          </button>
        </form>
      )}
      {error && (
        <p id={`${id}-error`} role="alert" className="text-sm text-danger">
          {error}
        </p>
      )}
      {spec.entry === nodeID ? (
        <p className="text-sm text-ink">{s.canvas.node.isEntry}</p>
      ) : (
        !readOnly && (
          <button type="button" className={`${secondary} self-start`} onClick={edits.setEntry}>
            {s.canvas.node.startHere}
          </button>
        )
      )}
      <NodeFields node={node} bindings={spec.bindings} readOnly={readOnly} onField={edits.setField} onModel={edits.setModel} />
      <NodeWays node={node} spec={spec} readOnly={readOnly} edits={edits} />
      {problems.length > 0 && (
        <div data-node-problems>
          <p className="text-sm font-medium text-danger">{s.canvas.node.problems(problems.length)}</p>
          <ul className="mt-1 flex flex-col gap-1">
            {problems.map((p) => (
              <li key={`${p.path}-${p.code}-${p.line}-${p.column}`}>
                <button
                  type="button"
                  className="text-left text-sm text-danger underline-offset-2 hover:underline"
                  onClick={() => edits.focusAt(p.line, p.column)}
                  data-problem={p.code}
                >
                  {p.message}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
      <div className="flex flex-col gap-2">
        <p className="text-xs text-ink-muted">{s.canvas.node.yamlHint}</p>
        <div className="flex flex-wrap gap-2">
          <button type="button" className={secondary} onClick={edits.openYaml} data-action="edit-in-yaml">
            {s.canvas.node.editYaml}
          </button>
          {!readOnly && !removing && (
            <button type="button" className={danger} onClick={() => setRemoving(true)}>
              {s.canvas.node.remove}
            </button>
          )}
        </div>
        {removing && (
          <div role="group" aria-labelledby={`${id}-remove`} className="flex flex-col gap-2 rounded-control border border-danger bg-danger-subtle p-3">
            <p id={`${id}-remove`} className="text-sm text-ink">
              {s.canvas.node.confirmRemove(nodeID)}
            </p>
            <div className="flex gap-2">
              <button type="button" className={danger} onClick={edits.remove}>
                {s.canvas.node.confirm}
              </button>
              <button type="button" className={secondary} onClick={() => setRemoving(false)}>
                {s.canvas.node.keep}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
