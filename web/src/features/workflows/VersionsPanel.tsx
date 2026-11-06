import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";
import { diffQuery, versionsQuery } from "@/api/workflows";
import { s } from "@/strings";
import { formatDateTime } from "@/lib/dates";
import { DiffHunks } from "./DiffHunks";

const card = "rounded-overlay border border-border bg-surface p-4 shadow-1";
const select = "h-8 rounded-control border border-border-strong bg-surface px-2 text-sm text-ink";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";

/** A workflow's versions, what changed between any two, and a way to open an older one in the editor. */
export function VersionsPanel({ workflowID, current, onOpen }: { workflowID: string; current: number; onOpen: (version: number) => void }) {
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
              <span className="text-xs text-ink-muted">{s.workflows.versions.by(v.createdBy.name, formatDateTime(v.createdAt))}</span>
              <button type="button" className={secondary} onClick={() => onOpen(v.number)} data-action="open-version">
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
          <DiffHunks hunks={diff.data.hunks} label={s.workflows.versions.diffLabel(compared.from, compared.to)} />
        </div>
      )}
    </section>
  );
}
