import type { components } from "@/api/schema";
import { s } from "@/strings";

type Hunk = components["schemas"]["DiffHunk"];
const MARK = { equal: " ", insert: "+", delete: "-" } as const;

/** The changed lines of one text between two versions, each run of them under the lines it starts at; workflows and skills share it. */
export function DiffHunks({ hunks, label }: { hunks: Hunk[]; label: string }) {
  return (
    <>
      {hunks.map((hunk) => (
        <figure key={`${hunk.fromLine}-${hunk.toLine}`} className="mb-3">
          <figcaption className="mb-1 text-xs text-ink-muted">{s.workflows.versions.lines(hunk.fromLine, hunk.toLine)}</figcaption>
          <pre className="overflow-x-auto rounded-control border border-border bg-surface-sunken p-2 font-mono text-xs text-ink" aria-label={label}>
            {hunk.lines.map((line, i) => (
              <span
                // biome-ignore lint/suspicious/noArrayIndexKey: lines of one hunk keep their order and never move.
                key={i}
                className={line.op === "delete" ? "block text-danger" : line.op === "insert" ? "block font-semibold text-accent" : "block"}
                data-op={line.op}
              >
                {MARK[line.op]} {line.text}
              </span>
            ))}
          </pre>
        </figure>
      ))}
    </>
  );
}
