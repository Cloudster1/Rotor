import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useId } from "react";
import { RUN_STATUSES, type RunFilter, type RunStatus, runsQuery } from "@/api/runs";
import { workflowsQuery } from "@/api/workflows";
import { EmptyState, PageHeader } from "@/features/shell/EmptyState";
import { Icon } from "@/features/shell/icons";
import { s } from "@/strings";
import { RunStatusBadge } from "./RunStatusBadge";

const select = "h-9 rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";

/** The search a runs list keeps its filters in, so a filtered list can be linked and reloaded. */
export type RunsSearch = RunFilter;

/** Reads the filters from the address, dropping anything that is not one. */
export function runsSearch(search: Record<string, unknown>): RunsSearch {
  const status = typeof search.status === "string" && (RUN_STATUSES as string[]).includes(search.status) ? (search.status as RunStatus) : undefined;
  return {
    workflow: typeof search.workflow === "string" && search.workflow !== "" ? search.workflow : undefined,
    status,
    mine: search.mine === true || search.mine === "true" ? true : undefined,
  };
}

function when(at: string | null): string {
  return at ? new Date(at).toLocaleString() : "";
}

/** The runs the person may see, newest first, narrowed by workflow, state and whether they started them. */
export function RunsPage({ search }: { search: RunsSearch }) {
  const id = useId();
  const navigate = useNavigate();
  const runs = useQuery(runsQuery(search));
  const workflows = useQuery(workflowsQuery(false));
  const narrow = (change: Partial<RunsSearch>) => void navigate({ to: "/runs", search: { ...search, ...change } });
  const filtered = search.workflow !== undefined || search.status !== undefined || search.mine !== undefined;
  const Glyph = Icon.Run;
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={s.places.runs} />
      <form className="flex flex-wrap items-end gap-4" aria-label={s.runs.list.filters} onSubmit={(e) => e.preventDefault()}>
        <label className="flex flex-col gap-1 text-sm font-medium text-ink" htmlFor={`${id}-workflow`}>
          {s.runs.list.workflow}
          <select id={`${id}-workflow`} className={select} value={search.workflow ?? ""} onChange={(e) => narrow({ workflow: e.target.value || undefined })}>
            <option value="">{s.runs.list.anyWorkflow}</option>
            {(workflows.data ?? []).map((w) => (
              <option key={w.id} value={w.id}>
                {w.name}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-1 text-sm font-medium text-ink" htmlFor={`${id}-status`}>
          {s.runs.list.status}
          <select
            id={`${id}-status`}
            className={select}
            value={search.status ?? ""}
            onChange={(e) => narrow({ status: (e.target.value || undefined) as RunStatus | undefined })}
          >
            <option value="">{s.runs.list.anyStatus}</option>
            {RUN_STATUSES.map((status) => (
              <option key={status} value={status}>
                {s.runs.status[status]}
              </option>
            ))}
          </select>
        </label>
        <label className="flex h-9 items-center gap-2 text-sm text-ink">
          <input type="checkbox" checked={search.mine === true} onChange={(e) => narrow({ mine: e.target.checked || undefined })} data-filter-mine />
          {s.runs.list.mine}
        </label>
      </form>
      {runs.isPending && <p className="text-sm text-ink-muted">{s.runs.list.loading}</p>}
      {runs.isError && (
        <p role="alert" className="text-sm text-danger">
          {s.runs.list.unreachable}
        </p>
      )}
      {runs.data && runs.data.length === 0 && (
        <EmptyState
          icon={<Glyph />}
          title={filtered ? s.runs.list.noneTitle : s.runs.emptyTitle}
          description={filtered ? s.runs.list.noneBody : s.runs.emptyBody}
        />
      )}
      {runs.data && runs.data.length > 0 && (
        <ul className="flex max-w-4xl flex-col gap-3" aria-label={s.runs.list.label} data-runs>
          {runs.data.map((r) => (
            <li key={r.id} className="rounded-overlay border border-border bg-surface p-4 shadow-1" data-run={r.id}>
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <Link to="/runs/$runID" params={{ runID: r.id }} className="font-medium text-accent hover:underline">
                  {s.runs.page.title(r.workflow.name)}
                </Link>
                <RunStatusBadge status={r.status} />
              </div>
              <p className="mt-1 text-xs text-ink-muted">
                {s.runs.page.startedBy(r.startedBy.name)} . {s.runs.list.created(when(r.createdAt))}
                {r.finishedAt && ` . ${s.runs.list.finished(when(r.finishedAt))}`}
              </p>
              {r.reason && <p className="mt-1 text-xs text-ink-muted">{r.reason}</p>}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
