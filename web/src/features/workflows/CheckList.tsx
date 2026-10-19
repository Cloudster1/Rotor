import type { Problem } from "@/api/workflows";
import { s } from "@/strings";

/** What checking found: problems, which stop a save, then warnings, which do not (#40); each jumps to its place. */
export function CheckList({
  checking,
  problems,
  warnings,
  focusAt,
}: {
  checking: boolean;
  problems: Problem[];
  warnings: Problem[];
  focusAt: (line: number, column: number) => void;
}) {
  if (checking) return <p className="text-sm text-ink-muted">{s.workflows.editor.checking}</p>;
  return (
    <>
      {problems.length === 0 ? (
        <p className="text-sm text-ink">{s.workflows.editor.valid}</p>
      ) : (
        <Found heading={s.workflows.editor.problems(problems.length)} items={problems} tone="text-danger" kind="problem" focusAt={focusAt} />
      )}
      {warnings.length > 0 && (
        <div className="mt-3 rounded-control border border-warning bg-warning-subtle px-3 py-2" data-warnings>
          <Found heading={s.untrusted.warnings(warnings.length)} items={warnings} tone="text-ink" kind="warning" focusAt={focusAt} />
          <p className="mt-1 text-xs text-ink">{s.untrusted.warningsHint}</p>
        </div>
      )}
    </>
  );
}

function Found({
  heading,
  items,
  tone,
  kind,
  focusAt,
}: {
  heading: string;
  items: Problem[];
  tone: string;
  kind: "problem" | "warning";
  focusAt: (line: number, column: number) => void;
}) {
  return (
    <>
      <p className={`text-sm font-medium ${tone}`}>{heading}</p>
      <ul className="mt-1 flex flex-col gap-1">
        {items.map((p) => (
          <li key={`${p.path}-${p.code}-${p.line}-${p.column}`}>
            <button
              type="button"
              className={`text-left text-sm ${tone} underline-offset-2 hover:underline`}
              onClick={() => focusAt(p.line, p.column)}
              {...{ [`data-${kind}`]: p.code }}
            >
              {p.line > 0 ? `${s.workflows.editor.at(p.line, p.column)}: ` : ""}
              {p.message}
            </button>
          </li>
        ))}
      </ul>
    </>
  );
}
