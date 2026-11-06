import { useQuery } from "@tanstack/react-query";
import type { Run } from "@/api/runs";
import { runTraceQuery, runTraceSpanQuery, traceSettingQuery } from "@/api/traces";
import { RUN_POLL_MS, TRACE_SETTLE_MS } from "@/config";
import { s } from "@/strings";
import { TraceBody } from "./TraceBody";

const TITLE_ID = "run-trace-title";

/** Spans may still arrive while the run goes on and for a short while after it ended. */
function stillArriving(run: Run): boolean {
  if (!run.ended) return true;
  return run.finishedAt !== null && Date.now() - Date.parse(run.finishedAt) < TRACE_SETTLE_MS;
}

/** A run's trace on its page (#245): its spans as a nested timeline, what Rotor kept of it, and where else it went. */
export function TraceSection({ run }: { run: Run }) {
  const trace = useQuery(runTraceQuery(run.id, () => (stillArriving(run) ? RUN_POLL_MS : false)));
  const setting = useQuery(traceSettingQuery);
  return (
    <section aria-labelledby={TITLE_ID} className="flex min-w-0 flex-col gap-3" data-run-trace>
      <h2 id={TITLE_ID} className="text-lg font-semibold text-ink">
        {s.trace.title}
      </h2>
      <p className="text-sm text-ink-muted">{s.trace.intro}</p>
      {trace.isError && <p className="text-sm text-ink-muted">{s.trace.unreadable(trace.error.message)}</p>}
      {trace.isPending && <p className="text-sm text-ink-muted">{s.trace.loading}</p>}
      {trace.data && (
        <TraceBody
          trace={trace.data}
          spanQuery={(spanID) => runTraceSpanQuery(run.id, spanID)}
          openFailure={run.status === "failed"}
          none={run.ended ? s.trace.none : s.trace.noneYet}
          labelledBy={TITLE_ID}
          days={setting.data?.retentionDays ?? null}
        />
      )}
    </section>
  );
}
