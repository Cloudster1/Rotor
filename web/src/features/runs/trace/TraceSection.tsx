import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { meQuery } from "@/api/auth";
import type { Run } from "@/api/runs";
import { runTraceQuery, type Trace, traceSettingQuery } from "@/api/traces";
import { RUN_POLL_MS, TRACE_SETTLE_MS } from "@/config";
import { DATE_AND_TIME, formatDateTime } from "@/lib/dates";
import { s } from "@/strings";
import { SpanDetails } from "./SpanDetails";
import { SpanTree } from "./SpanTree";
import { firstFailure, indexSpans, type MoreRow, CHILDREN_PAGE, traceWindow, visibleRows } from "./tree";

const TITLE_ID = "run-trace-title";
const DETAILS_ID = "run-trace-details-title";
const notice = "rounded-control border border-warning bg-warning-subtle px-3 py-2 text-sm text-ink";

/** Spans may still arrive while the run goes on and for a short while after it ended. */
function stillArriving(run: Run): boolean {
  if (!run.ended) return true;
  return run.finishedAt !== null && Date.now() - Date.parse(run.finishedAt) < TRACE_SETTLE_MS;
}

function Notices({ trace, run, days }: { trace: Trace; run: Run; days: number | null }) {
  const t = s.trace;
  // Only an administrator can change it, so only an administrator is pointed to the page.
  const admin = useQuery(meQuery).data?.role === "admin";
  const expires = trace.expiresAt ? formatDateTime(trace.expiresAt, DATE_AND_TIME) : null;
  return (
    <>
      {trace.state === "none" && (
        <p className="text-sm text-ink-muted" data-trace-state="none">
          {run.ended ? t.none : t.noneYet}
        </p>
      )}
      {trace.state === "expired" && (
        <p className={notice} data-trace-state="expired">
          {t.expired(expires, days)} {t.spans(trace.spans)}.
        </p>
      )}
      {trace.state === "kept" && (
        <p className="text-sm text-ink-muted" data-trace-state="kept">
          {t.spans(trace.spans)}. {expires && t.keptUntil(expires, days)}
        </p>
      )}
      {trace.cut && (
        <p className={notice} data-trace-cut={trace.dropped}>
          {t.cut(trace.dropped)}
        </p>
      )}
      {trace.state === "kept" && !trace.content && (
        <p className="text-sm text-ink-muted" data-trace-no-text>
          {t.noText}
          {admin && (
            <>
              {" "}
              <Link to="/admin/traces" className="font-medium text-accent underline-offset-2 hover:underline" data-trace-choose-text>
                {t.chooseText}
              </Link>
            </>
          )}
        </p>
      )}
      {trace.links.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 text-sm text-ink" data-trace-links>
          <span>{t.links}</span>
          <ul className="flex flex-wrap gap-3">
            {trace.links.map((link) => (
              <li key={link.url}>
                <a href={link.url} target="_blank" rel="noreferrer" className="font-medium text-accent underline-offset-2 hover:underline" data-trace-link>
                  {t.openIn(link.destination)}
                </a>
              </li>
            ))}
          </ul>
        </div>
      )}
    </>
  );
}

/** The spans of a kept trace and the chosen one's details; when the run failed, the span it failed in is chosen. */
function Spans({ trace, run }: { trace: Trace; run: Run }) {
  const [chosen, setChosen] = useState<string | null>(null);
  const [toggled, setToggled] = useState<ReadonlySet<string>>(new Set());
  const [shown, setShown] = useState<ReadonlyMap<string, number>>(new Map());
  const index = useMemo(() => indexSpans(trace.roots), [trace.roots]);
  const failure = useMemo(() => (run.status === "failed" ? firstFailure(index) : null), [index, run.status]);
  const path = useMemo(() => new Set(failure ? [...failure.ancestors, failure.span.spanId] : []), [failure]);
  const rows = useMemo(() => visibleRows(trace.roots, { total: index.size, toggled, shown, path }), [trace.roots, index.size, toggled, shown, path]);
  const timeline = useMemo(() => traceWindow(trace, index), [trace, index]);
  const selected = chosen ?? failure?.span.spanId ?? null;
  const span = selected ? index.get(selected)?.span : undefined;

  const toggle = (id: string) =>
    setToggled((before) => {
      const after = new Set(before);
      if (!after.delete(id)) after.add(id);
      return after;
    });
  const more = (row: MoreRow) => setShown((before) => new Map(before).set(row.parent, row.shown + CHILDREN_PAGE));

  return (
    <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
      <SpanTree rows={rows} timeline={timeline} selected={selected} labelledBy={TITLE_ID} onSelect={setChosen} onToggle={toggle} onMore={more} />
      {span ? (
        <SpanDetails key={span.spanId} runID={run.id} span={span} traceStart={timeline.start} textKept={trace.content} headingID={DETAILS_ID} />
      ) : (
        <p className="text-sm text-ink-muted">{s.trace.details.empty}</p>
      )}
    </div>
  );
}

/** A run's trace on its page (#245): its spans as a nested timeline, what Rotor kept of it, and where else it went. */
export function TraceSection({ run }: { run: Run }) {
  const trace = useQuery(runTraceQuery(run.id, () => (stillArriving(run) ? RUN_POLL_MS : false)));
  const setting = useQuery(traceSettingQuery);
  const days = setting.data?.retentionDays ?? null;
  return (
    <section aria-labelledby={TITLE_ID} className="flex min-w-0 flex-col gap-3" data-run-trace>
      <h2 id={TITLE_ID} className="text-lg font-semibold text-ink">
        {s.trace.title}
      </h2>
      <p className="text-sm text-ink-muted">{s.trace.intro}</p>
      {trace.isError && <p className="text-sm text-ink-muted">{s.trace.unreadable(trace.error.message)}</p>}
      {trace.isPending && <p className="text-sm text-ink-muted">{s.trace.loading}</p>}
      {trace.data && <Notices trace={trace.data} run={run} days={days} />}
      {trace.data?.state === "kept" && trace.data.roots.length > 0 && <Spans trace={trace.data} run={run} />}
    </section>
  );
}
