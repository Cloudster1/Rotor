import { type FormEvent, useId, useState } from "react";
import { s } from "@/strings";
import { CanvasDialog } from "./CanvasDialog";
import { input, label, secondary } from "./styles";

const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover";

/** Asks how a router goes to the node it was connected to: by a case for one value, or as its default. */
export function CaseDialog({
  from,
  to,
  current,
  taken,
  onCase,
  onDefault,
  onClose,
}: {
  from: string;
  to: string;
  /** Where the router's default leads now, if it has one. */
  current?: string;
  taken: readonly string[];
  onCase: (value: string) => void;
  onDefault: () => void;
  onClose: () => void;
}) {
  const id = useId();
  const [value, setValue] = useState("");
  const [error, setError] = useState<string>();

  function submit(event: FormEvent) {
    event.preventDefault();
    const v = value.trim();
    const problem = v === "" ? s.canvas.picker.needCase : taken.includes(v) ? s.canvas.node.caseTaken(v) : undefined;
    setError(problem);
    if (!problem) onCase(v);
  }

  return (
    <CanvasDialog title={s.canvas.cases.title(from, to)} onClose={onClose} data-case-dialog="">
      <p className="text-sm text-ink-muted">{s.canvas.cases.intro}</p>
      <form onSubmit={submit} className="flex flex-col gap-3" noValidate>
        <label className={label}>
          {s.canvas.cases.value}
          <input
            className={input}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            aria-invalid={error !== undefined}
            aria-describedby={error ? `${id}-error` : undefined}
            data-case-value
          />
        </label>
        {error && (
          <p id={`${id}-error`} role="alert" className="text-xs text-danger">
            {error}
          </p>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" className={secondary} onClick={onClose}>
            {s.canvas.cases.cancel}
          </button>
          <button type="button" className={secondary} onClick={onDefault} data-case-default>
            {current ? s.canvas.cases.replaceDefault(current) : s.canvas.cases.makeDefault}
          </button>
          <button type="submit" className={primary} data-case-add>
            {s.canvas.cases.add}
          </button>
        </div>
      </form>
    </CanvasDialog>
  );
}
