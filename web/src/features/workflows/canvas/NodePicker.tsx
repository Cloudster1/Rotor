import { useId, useState } from "react";
import { s } from "@/strings";
import { CanvasDialog } from "./CanvasDialog";
import { NODE_TYPES, type NodeType } from "./specDoc";
import { input, label, secondary } from "./styles";

const choice =
  "flex w-full flex-col items-start gap-0.5 rounded-control border border-border-strong bg-surface px-3 py-2 text-left hover:border-accent hover:bg-surface-raised focus-visible:border-accent";

/** Asks which kind of node to add; when a router with a default leads to it, also the value of the case it becomes. */
export function NodePicker({
  title,
  caseOf,
  onPick,
  onClose,
}: {
  title: string;
  /** The router the new node becomes a case of, and the values its cases match already. */
  caseOf?: { router: string; taken: readonly string[] };
  onPick: (type: NodeType, caseValue?: string) => void;
  onClose: () => void;
}) {
  const id = useId();
  const [value, setValue] = useState("");
  const [error, setError] = useState<string>();

  function pick(type: NodeType) {
    if (caseOf) {
      const v = value.trim();
      const problem = v === "" ? s.canvas.picker.needCase : caseOf.taken.includes(v) ? s.canvas.node.caseTaken(v) : undefined;
      setError(problem);
      if (problem) return;
      onPick(type, v);
      return;
    }
    onPick(type);
  }

  return (
    <CanvasDialog title={title} onClose={onClose} data-node-picker="">
      {caseOf && (
        <div className="flex flex-col gap-1">
          <label className={label}>
            {s.canvas.picker.caseValue(caseOf.router)}
            <input
              className={input}
              value={value}
              onChange={(e) => setValue(e.target.value)}
              aria-invalid={error !== undefined}
              aria-describedby={`${id}-case`}
              data-picker-case
            />
          </label>
          <p id={`${id}-case`} className={`text-xs ${error ? "text-danger" : "text-ink-muted"}`} role={error ? "alert" : undefined}>
            {error ?? s.canvas.picker.caseHint}
          </p>
        </div>
      )}
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 text-sm font-medium text-ink">{s.canvas.picker.kind}</legend>
        <ul className="grid gap-2 sm:grid-cols-2">
          {NODE_TYPES.map((type) => (
            <li key={type}>
              <button type="button" className={choice} onClick={() => pick(type)} data-pick={type}>
                <span className="text-sm font-medium text-ink">{s.canvas.types[type]}</span>
                <span className="text-xs text-ink-muted">{s.canvas.kinds[type]}</span>
              </button>
            </li>
          ))}
        </ul>
      </fieldset>
      <div className="flex justify-end">
        <button type="button" className={secondary} onClick={onClose}>
          {s.canvas.picker.cancel}
        </button>
      </div>
    </CanvasDialog>
  );
}
