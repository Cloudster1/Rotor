import { useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";
import { meQuery } from "@/api/auth";
import { type AuditEntry, type AuditFilter, auditExportHref, auditFacetsQuery, auditQuery } from "@/api/audit";
import { ApiError } from "@/api/client";
import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";

// Adapted from Exciter's audit page (c0d072d): the same filters and export, walked by the API's keyset cursor.

const select = "h-9 w-full rounded-control border border-border-strong bg-surface px-2 text-sm text-ink";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";
const link = "inline-flex h-8 items-center rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised";

/** The audit log for administrators; anyone else is told it is not theirs to read. */
export function AuditRoute() {
  const me = useQuery(meQuery);
  if (!me.data) return <p className="text-sm text-ink-muted">{s.audit.loading}</p>;
  if (me.data.role !== "admin") {
    return (
      <div>
        <PageHeader title={s.audit.title} />
        <p role="alert" className="text-sm text-danger">
          {s.audit.notAllowed}
        </p>
      </div>
    );
  }
  return <AuditLog />;
}

/** What the entry's data says to a reader, without its ids. */
export function describe(entry: AuditEntry): string {
  const parts: string[] = [];
  for (const [key, value] of Object.entries((entry.data ?? {}) as Record<string, unknown>)) {
    if (/id$/i.test(key) || value === null || value === undefined || value === "") continue;
    if (Array.isArray(value)) parts.push(`${key}: ${value.join(", ")}`);
    else if (typeof value !== "object") parts.push(`${key}: ${String(value)}`);
  }
  if (entry.ip) parts.push(s.audit.fromAddress(entry.ip));
  return parts.join("; ");
}

function when(at: string): string {
  return new Date(at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "medium" });
}

/** Filters, one window of entries, paging back and forth by cursor, and the two exports. */
export function AuditLog() {
  const id = useId();
  const [filter, setFilter] = useState<AuditFilter>({});
  // Each window is reached by the cursor the one before answered; going back pops it.
  const [cursors, setCursors] = useState<string[]>([]);
  const log = useQuery(auditQuery(filter, cursors[cursors.length - 1]));
  const facets = useQuery(auditFacetsQuery);
  const entries = log.data?.entries ?? [];
  const next = log.data?.next ?? null;
  const problem = log.error instanceof ApiError ? log.error : null;
  const filtered = Object.values(filter).some(Boolean);
  const narrow = (patch: Partial<AuditFilter>) => {
    setFilter((current) => ({ ...current, ...patch }));
    setCursors([]);
  };
  const field = (name: string) => `${id}-${name}`;

  return (
    <div className="flex flex-col gap-4" data-audit-log>
      <PageHeader title={s.audit.title} />
      <p className="max-w-3xl text-sm text-ink-muted">{s.audit.intro}</p>
      <section aria-label={s.audit.filters} className="flex flex-col gap-3" data-audit-filters>
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
          <div className="flex flex-col gap-1">
            <label htmlFor={field("action")} className="text-sm font-medium text-ink">
              {s.audit.action}
            </label>
            <select id={field("action")} className={select} value={filter.action ?? ""} onChange={(e) => narrow({ action: e.target.value || undefined })}>
              <option value="">{s.audit.everyAction}</option>
              {(facets.data?.actions ?? []).map((action) => (
                <option key={action} value={action}>
                  {s.audit.actions[action] ?? action}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <label htmlFor={field("actor")} className="text-sm font-medium text-ink">
              {s.audit.actor}
            </label>
            <select id={field("actor")} className={select} value={filter.actor ?? ""} onChange={(e) => narrow({ actor: e.target.value || undefined })}>
              <option value="">{s.audit.everybody}</option>
              {(facets.data?.actors ?? []).map((actor) => (
                <option key={actor.id} value={actor.id}>
                  {actor.name || actor.id}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <label htmlFor={field("type")} className="text-sm font-medium text-ink">
              {s.audit.targetType}
            </label>
            <select id={field("type")} className={select} value={filter.targetType ?? ""} onChange={(e) => narrow({ targetType: e.target.value || undefined })}>
              <option value="">{s.audit.everyTarget}</option>
              {(facets.data?.targetTypes ?? []).map((type) => (
                <option key={type} value={type}>
                  {type}
                </option>
              ))}
            </select>
          </div>
          {(["from", "to"] as const).map((name) => (
            <div key={name} className="flex flex-col gap-1">
              <label htmlFor={field(name)} className="text-sm font-medium text-ink">
                {s.audit[name]}
              </label>
              <input
                id={field(name)}
                type="date"
                className={select}
                value={filter[name] ?? ""}
                onChange={(e) => narrow({ [name]: e.target.value || undefined })}
                aria-invalid={problem?.fields[name] ? true : undefined}
                aria-describedby={problem?.fields[name] ? field(`${name}-error`) : undefined}
              />
              {problem?.fields[name] && (
                <p id={field(`${name}-error`)} className="text-sm text-danger">
                  {problem.fields[name]}
                </p>
              )}
            </div>
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {filter.target && (
            <button
              type="button"
              className={secondary}
              onClick={() => narrow({ target: undefined })}
              aria-label={s.audit.showEveryTarget}
              data-audit-target-filter
            >
              {s.audit.onlyTarget(filter.target)} x
            </button>
          )}
          {filtered && (
            <button
              type="button"
              className={secondary}
              onClick={() => narrow({ action: undefined, actor: undefined, targetType: undefined, target: undefined, from: undefined, to: undefined })}
              data-action="clear-audit-filters"
            >
              {s.audit.clear}
            </button>
          )}
          <span className="ml-auto flex gap-2">
            <a className={link} href={auditExportHref(filter, "csv")} download data-action="export-audit-csv">
              {s.audit.exportCSV}
            </a>
            <a className={link} href={auditExportHref(filter, "json")} download data-action="export-audit-json">
              {s.audit.exportJSON}
            </a>
          </span>
        </div>
      </section>
      {log.isError && !problem?.fields.from && !problem?.fields.to && (
        <p role="alert" className="text-sm text-danger">
          {log.error.message}
        </p>
      )}
      {log.isPending && <p className="text-sm text-ink-muted">{s.audit.loading}</p>}
      {log.data && entries.length === 0 && <p className="text-sm text-ink-muted">{filtered ? s.audit.emptyFiltered : s.audit.empty}</p>}
      {entries.length > 0 && (
        <div className="overflow-x-auto rounded-overlay border border-border bg-surface shadow-1">
          <table className="w-full text-left text-sm" data-audit-table>
            <thead className="border-b border-border text-ink-muted">
              <tr>
                <th scope="col" className="px-3 py-2 font-medium">
                  {s.audit.columnWhen}
                </th>
                <th scope="col" className="px-3 py-2 font-medium">
                  {s.audit.columnWhat}
                </th>
                <th scope="col" className="px-3 py-2 font-medium">
                  {s.audit.columnWho}
                </th>
                <th scope="col" className="px-3 py-2 font-medium">
                  {s.audit.columnTarget}
                </th>
                <th scope="col" className="px-3 py-2 font-medium">
                  {s.audit.columnDetails}
                </th>
              </tr>
            </thead>
            <tbody>
              {entries.map((entry) => (
                <AuditRow key={entry.id} entry={entry} onActor={(actor) => narrow({ actor })} onTarget={(target) => narrow({ target })} />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {(cursors.length > 0 || next) && (
        <nav aria-label={s.audit.pages} className="flex items-center justify-end gap-2">
          <span className="mr-auto text-sm text-ink-muted" data-audit-page>
            {s.audit.page(cursors.length + 1)}
          </span>
          <button
            type="button"
            className={secondary}
            disabled={cursors.length === 0}
            onClick={() => setCursors(cursors.slice(0, -1))}
            data-action="audit-newer"
          >
            {s.audit.newer}
          </button>
          <button type="button" className={secondary} disabled={!next} onClick={() => next && setCursors([...cursors, next])} data-action="audit-older">
            {s.audit.older}
          </button>
        </nav>
      )}
    </div>
  );
}

/** One entry as a row; the person and the target narrow the log to themselves when followed. */
function AuditRow({ entry, onActor, onTarget }: { entry: AuditEntry; onActor: (actor: string) => void; onTarget: (target: string) => void }) {
  const actor = entry.actor;
  return (
    <tr className="border-b border-border align-top last:border-0" data-audit-row={entry.action}>
      <td className="px-3 py-2 whitespace-nowrap text-ink-muted">
        <time dateTime={entry.at}>{when(entry.at)}</time>
      </td>
      <td className="px-3 py-2 text-ink">
        {s.audit.actions[entry.action] ?? entry.action}
        <span className="block font-mono text-xs text-ink-muted">{entry.action}</span>
      </td>
      <td className="px-3 py-2">
        {actor ? (
          <button
            type="button"
            className="text-left text-ink underline-offset-2 hover:underline"
            aria-label={s.audit.filterByActor(actor.name || actor.id)}
            onClick={() => onActor(actor.id)}
          >
            {actor.name || actor.id}
          </button>
        ) : (
          <span className="text-ink-muted">{s.audit.rotor}</span>
        )}
        {entry.via && (
          <span className="block text-xs text-ink-muted" data-audit-via>
            {s.audit.through(entry.via)}
          </span>
        )}
      </td>
      <td className="px-3 py-2">
        <span className="block text-xs text-ink-muted">{entry.targetType}</span>
        {entry.targetId && (
          <button
            type="button"
            className="max-w-48 truncate text-left font-mono text-xs text-ink underline-offset-2 hover:underline"
            aria-label={s.audit.filterByTarget(entry.targetId)}
            onClick={() => onTarget(entry.targetId)}
            data-action="filter-target"
          >
            {entry.targetId}
          </button>
        )}
      </td>
      <td className="px-3 py-2 text-xs break-words text-ink-muted">{describe(entry)}</td>
    </tr>
  );
}
