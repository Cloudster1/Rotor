import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";
import { diffQuery, fetchVersion, versionsQuery } from "@/api/workflows";
import { s } from "@/strings";

const card = "rounded-overlay border border-border bg-surface p-4 shadow-1";
const select = "h-8 rounded-control border border-border-strong bg-surface px-2 text-sm text-ink";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";
const MARK = { equal: " ", insert: "+", delete: "-" } as const;

/** A workflow's versions, what changed between any two, and a way to open an older one in the editor. */
export function VersionsPanel({ workflowID, current, onOpen }: { workflowID: string; current: number; onOpen: (yaml: string) => void }) {
  const id = useId();
  const versions = useQuery(versionsQuery(workflowID));
  const [from, setFrom] = useState(Math.max(current - 1, 1));
  const [to, setTo] = useState(current);
  const [compared, setCompared] = useState<{ from: number; to: number } | null>(null);
  const diff = useQuery({ ...diffQuery(workflowID, compared?.from ?? 0, compared?.to ?? 0), enabled: compared !== null });
  const numbers = (versions.data ?? []).map((v) => v.number);
  return (
    <section className={card} aria-labelledby={`${id}-title`} data-versions>
      <h2 id={`${id}-title`} className="mb-3 text-lg font-semibold text-ink">
        {s.workflows.versions.title}
      </h2>
      <ol className="mb-4 flex flex-col gap-2">
        {(versions.data ?? []).map((v) => (
          <li key={v.number} className="flex flex-wrap items-baseline justify-between gap-2 text-sm" data-version={v.number}>
            <span className="text-ink">
              <span className="font-medium">{s.workflows.version(v.number)}</span>
              {v.comment && <span className="text-ink-muted"> {v.comment}</span>}
            </span>
            <span className="flex items-center gap-2">
              <span className="text-xs text-ink-muted">{s.workflows.versions.by(v.createdBy.name, new Date(v.createdAt).toLocaleString())}</span>
              <button type="button" className={secondary} onClick={async () => onOpen((await fetchVersion(workflowID, v.number)).specYAML)}>
                {s.workflows.versions.open}
                <span className="sr-only"> {s.workflows.version(v.number)}</span>
              </button>
            </span>
          </li>
        ))}
      </ol>
      {numbers.length > 1 && (
        <div className="flex flex-wrap items-end gap-3">
          <label className="flex flex-col gap-1 text-sm text-ink">
            {s.workflows.versions.from}
            <select className={select} value={from} onChange={(e) => setFrom(Number(e.target.value))} data-diff-from>
              {numbers.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
          <label className="flex flex-col gap-1 text-sm text-ink">
            {s.workflows.versions.to}
            <select className={select} value={to} onChange={(e) => setTo(Number(e.target.value))} data-diff-to>
              {numbers.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
          </label>
          <button type="button" className={secondary} onClick={() => setCompared({ from, to })} data-action="compare">
            {s.workflows.versions.compare}
          </button>
        </div>
      )}
      {compared && diff.data && (
        <div className="mt-4" data-diff>
          {diff.data.hunks.length === 0 && <p className="text-sm text-ink-muted">{s.workflows.versions.same}</p>}
          {diff.data.hunks.map((hunk) => (
            <figure key={`${hunk.fromLine}-${hunk.toLine}`} className="mb-3">
              <figcaption className="mb-1 text-xs text-ink-muted">{s.workflows.versions.lines(hunk.fromLine, hunk.toLine)}</figcaption>
              <pre
                className="overflow-x-auto rounded-control border border-border bg-surface-sunken p-2 font-mono text-xs text-ink"
                aria-label={s.workflows.versions.diffLabel(compared.from, compared.to)}
              >
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
        </div>
      )}
    </section>
  );
}
