import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import { meQuery } from "@/api/auth";
import type { SpanQuery, Trace } from "@/api/traces";
import { DATE_AND_TIME, formatDateTime } from "@/lib/dates";
import { s } from "@/strings";
import { SpanDetails } from "./SpanDetails";
import { SpanTree } from "./SpanTree";
import { firstFailure, indexSpans, type MoreRow, CHILDREN_PAGE, traceWindow, visibleRows } from "./tree";

// The part of a trace's view a run's page and an owned trace's page share (#257): what Rotor kept, and the spans.

const DETAILS_ID = "trace-details-title";
const notice = "rounded-control border border-warning bg-warning-subtle px-3 py-2 text-sm text-ink";

function Notices({ trace, none, days }: { trace: Trace; none: string; days: number | null }) {
  const t = s.trace;
  // Only an administrator can change it, so only an administrator is pointed to the page.
  const admin = useQuery(meQuery).data?.role === "admin";
  const expires = trace.expiresAt ? formatDateTime(trace.expiresAt, DATE_AND_TIME) : null;
  return (
    <>
      {trace.state === "none" && (
        <p className="text-sm text-ink-muted" data-trace-state="none">
          {none}
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

interface SpansProps {
  trace: Trace;
  spanQuery: SpanQuery;
  openFailure: boolean;
  focus?: string;
  labelledBy: string;
}

/** The spans of a kept trace and the chosen one's details: the span focus names, else with openFailure the failed one. */
function Spans({ trace, spanQuery, openFailure, focus, labelledBy }: SpansProps) {
  const [chosen, setChosen] = useState<string | null>(null);
  const [toggled, setToggled] = useState<ReadonlySet<string>>(new Set());
  const [shown, setShown] = useState<ReadonlyMap<string, number>>(new Map());
  const index = useMemo(() => indexSpans(trace.roots), [trace.roots]);
  // A span the address names, as a search opens it, comes before the failure: the person asked for that one.
  const opened = useMemo(() => (focus && index.get(focus)) || (openFailure ? firstFailure(index) : null), [index, focus, openFailure]);
  const path = useMemo(() => new Set(opened ? [...opened.ancestors, opened.span.spanId] : []), [opened]);
  const rows = useMemo(() => visibleRows(trace.roots, { total: index.size, toggled, shown, path }), [trace.roots, index.size, toggled, shown, path]);
  const timeline = useMemo(() => traceWindow(trace, index), [trace, index]);
  const selected = chosen ?? opened?.span.spanId ?? null;
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
      <SpanTree rows={rows} timeline={timeline} selected={selected} labelledBy={labelledBy} onSelect={setChosen} onToggle={toggle} onMore={more} />
      {span ? (
        <SpanDetails key={span.spanId} spanQuery={spanQuery} span={span} traceStart={timeline.start} textKept={trace.content} headingID={DETAILS_ID} />
      ) : (
        <p className="text-sm text-ink-muted">{s.trace.details.empty}</p>
      )}
    </div>
  );
}

export interface TraceBodyProps {
  trace: Trace;
  /** Asks for one span of this trace whole. */
  spanQuery: SpanQuery;
  /** Whether to choose the span the trace failed in, and open the way to it. */
  openFailure: boolean;
  /** A span to choose and open the way to before any failure, as a search's hit names it. */
  focus?: string;
  /** What to say of a trace that holds no span yet. */
  none: string;
  /** The heading that names the tree. */
  labelledBy: string;
  /** How many days traces are kept, null while unknown; the host asks as it opens, so it comes with the trace. */
  days: number | null;
}

/** A trace's notices, then its spans and the chosen one's details once it holds any. */
export function TraceBody({ trace, spanQuery, openFailure, focus, none, labelledBy, days }: TraceBodyProps) {
  return (
    <>
      <Notices trace={trace} none={none} days={days} />
      {trace.state === "kept" && trace.roots.length > 0 && (
        <Spans trace={trace} spanQuery={spanQuery} openFailure={openFailure} focus={focus} labelledBy={labelledBy} />
      )}
    </>
  );
}
