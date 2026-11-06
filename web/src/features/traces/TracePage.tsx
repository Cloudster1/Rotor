import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { ownTraceQuery, ownTraceSpanQuery, type Trace, traceSettingQuery } from "@/api/traces";
import { TraceBody } from "@/features/runs/trace/TraceBody";
import { link } from "@/features/settings/styles";
import { PageHeader } from "@/features/shell/EmptyState";
import { DATE_AND_TIME, formatDateTime } from "@/lib/dates";
import { s } from "@/strings";

const SPANS_TITLE_ID = "own-trace-spans-title";

function Term({ term, children }: { term: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col">
      <dt className="text-xs font-medium text-ink-muted">{term}</dt>
      <dd className="text-sm break-all text-ink">{children}</dd>
    </div>
  );
}

/** Who sent the trace, from which service, and when it began; the earliest root names the service, as the list does. */
function About({ trace }: { trace: Trace }) {
  const t = s.traces.page;
  const root = trace.roots[0];
  return (
    <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-2 lg:grid-cols-4" data-own-trace-about>
      <Term term={t.id}>
        <span className="font-mono">{trace.traceId}</span>
      </Term>
      {trace.sender && <Term term={t.sender}>{trace.sender.name}</Term>}
      {root && <Term term={t.service}>{root.service}</Term>}
      {trace.startedAt && (
        <Term term={t.started}>
          <time dateTime={trace.startedAt}>{formatDateTime(trace.startedAt, DATE_AND_TIME)}</time>
        </Term>
      )}
    </dl>
  );
}

/** One trace a sender posted of no run (#257), in the tree and details a run's trace has; sender is whose it is. */
export function TracePage({ traceID, sender }: { traceID: string; sender: string }) {
  const t = s.traces.page;
  const trace = useQuery(ownTraceQuery(traceID, sender));
  const setting = useQuery(traceSettingQuery);
  const data = trace.data;
  return (
    <div className="flex flex-col gap-6" data-own-trace={traceID}>
      <Link to="/traces" className={link}>
        {t.back}
      </Link>
      <PageHeader title={t.title(data?.roots[0]?.name ?? "")} />
      {trace.isPending && <p className="text-sm text-ink-muted">{s.trace.loading}</p>}
      {trace.isError && (
        <p role="alert" className="text-sm text-danger">
          {s.trace.unreadable(trace.error.message)}
        </p>
      )}
      {data && (
        <>
          <About trace={data} />
          <section aria-labelledby={SPANS_TITLE_ID} className="flex min-w-0 flex-col gap-3">
            <h2 id={SPANS_TITLE_ID} className="text-lg font-semibold text-ink">
              {t.spansTitle}
            </h2>
            <p className="text-sm text-ink-muted">{t.intro}</p>
            {/* No run says whether it failed, so the span it failed in opens whenever one did: the person came to debug. */}
            <TraceBody
              trace={data}
              spanQuery={(spanID) => ownTraceSpanQuery(traceID, sender, spanID)}
              openFailure
              none={t.none}
              labelledBy={SPANS_TITLE_ID}
              days={setting.data?.retentionDays ?? null}
            />
          </section>
        </>
      )}
    </div>
  );
}
