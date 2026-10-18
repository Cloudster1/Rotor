import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { cancelRun, runQuery, runsQueryKey } from "@/api/runs";
import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";
import { RunGraph } from "./RunGraph";
import { RunStatusBadge } from "./RunStatusBadge";
import { RunTimeline } from "./RunTimeline";
import { entries, nodeStates } from "./timeline";
import { useRunEvents } from "./useRunEvents";

const secondary = "h-9 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";
const danger = "h-9 rounded-control bg-danger px-3 text-sm font-medium text-on-danger hover:bg-danger-hover disabled:opacity-60";

/** One run, live: where it is, its graph lit up node by node, what it did step by step, and cancelling it. */
export function RunPage({ runID }: { runID: string }) {
  const client = useQueryClient();
  const run = useQuery(runQuery(runID));
  // A run's state moves with its last event, so the page asks for the run again then.
  const { events, live } = useRunEvents(runID, (event) => {
    if (event.type === "run.finished" || event.type === "runner.lost") void client.invalidateQueries({ queryKey: runQuery(runID).queryKey });
  });
  const timeline = useMemo(() => entries(events), [events]);
  const states = useMemo(() => nodeStates(events), [events]);
  const [confirming, setConfirming] = useState(false);
  const cancel = useMutation({
    mutationFn: () => cancelRun(runID),
    onSuccess: (cancelled) => {
      client.setQueryData(runQuery(runID).queryKey, (before) => ({ ...cancelled, graph: before?.graph }));
      void client.invalidateQueries({ queryKey: runsQueryKey });
      setConfirming(false);
    },
  });
  if (run.isError) {
    return (
      <div>
        <PageHeader title={s.places.runs} />
        <p role="alert" className="text-sm text-danger">
          {s.runs.page.notFound}
        </p>
      </div>
    );
  }
  if (!run.data) return <p className="text-sm text-ink-muted">{s.runs.page.loading}</p>;
  const r = run.data;
  return (
    <div className="flex max-w-6xl flex-col gap-4">
      <PageHeader title={s.runs.page.title(r.workflow.name)} />
      <div className="flex flex-wrap items-center gap-3 text-sm text-ink-muted">
        <RunStatusBadge status={r.status} />
        <span>{s.runs.page.startedBy(r.startedBy.name)}</span>
        <Link to="/workflows/$workflowID" params={{ workflowID: r.workflow.id }} className="text-accent underline-offset-2 hover:underline">
          {r.workflow.name}, {s.runs.page.version(r.version)}
        </Link>
      </div>
      {!r.ended && <p className="text-sm text-ink-muted">{s.runs.page.live}</p>}
      {r.reason && (
        <p className="text-sm text-ink">
          <span className="font-medium">{s.runs.page.reason}: </span>
          {r.reason}
        </p>
      )}
      {!r.ended &&
        (confirming ? (
          <div role="group" aria-label={s.runs.page.confirmCancel} className="flex flex-wrap items-center gap-2">
            <span className="text-sm text-ink">{s.runs.page.confirmCancel}</span>
            <button type="button" className={danger} disabled={cancel.isPending} onClick={() => cancel.mutate()} data-action="confirm-cancel-run">
              {cancel.isPending ? s.runs.page.cancelling : s.runs.page.confirm}
            </button>
            <button type="button" className={secondary} onClick={() => setConfirming(false)}>
              {s.runs.page.keep}
            </button>
          </div>
        ) : (
          <div>
            <button type="button" className={secondary} onClick={() => setConfirming(true)} data-action="cancel-run">
              {s.runs.page.cancel}
            </button>
          </div>
        ))}
      {cancel.isError && (
        <p role="alert" className="text-sm text-danger">
          {cancel.error.message}
        </p>
      )}
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
        {r.graph && <RunGraph graph={r.graph} states={states} />}
        <RunTimeline entries={timeline} live={live && !r.ended} />
      </div>
      {r.output !== null && (
        <section className="flex flex-col gap-2">
          <h2 className="text-lg font-semibold text-ink">{s.runs.page.output}</h2>
          <pre className="overflow-x-auto rounded-control border border-border bg-surface-sunken p-3 text-xs text-ink" data-run-output>
            {JSON.stringify(r.output, null, 2)}
          </pre>
        </section>
      )}
    </div>
  );
}
