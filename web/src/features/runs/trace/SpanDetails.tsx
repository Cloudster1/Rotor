import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { SpanQuery, TraceSpan } from "@/api/traces";
import { formatTime } from "@/lib/dates";
import { s } from "@/strings";
import { SpanLinks } from "./SpanLinks";
import { Events, Pairs, Pre, TIME_WITH_SECONDS, Value } from "./SpanText";
import { isFailed } from "./tree";

const label = "text-xs font-medium text-ink-muted";

function Term({ term, children }: { term: string; children: ReactNode }) {
  return (
    <div className="flex flex-col">
      <dt className={label}>{term}</dt>
      <dd className="break-words text-sm text-ink">{children}</dd>
    </div>
  );
}

interface DetailsProps {
  /** Asks for a span of the trace whole, a run's or an owned one's. */
  spanQuery: SpanQuery;
  span: TraceSpan;
  /** When the trace began, in milliseconds, so a span says how far into it it started. */
  traceStart: number;
  /** Whether the trace kept text, so the span route has a whole input and output to answer. */
  textKept: boolean;
  headingID: string;
}

/** One span: what it was, when, how it ended, and what it read and wrote, the whole of each read when it opens. */
export function SpanDetails({ spanQuery, span, traceStart, textKept, headingID }: DetailsProps) {
  const d = s.trace.details;
  const whole = useQuery({ ...spanQuery(span.spanId), enabled: textKept });
  const failed = isFailed(span);
  const wholeValue = (which: "input" | "output") => (whole.data ? whole.data[which] : undefined);
  const error = whole.isError ? whole.error.message : null;
  const offset = Math.max(0, Date.parse(span.startedAt) - traceStart);
  return (
    <section aria-labelledby={headingID} className="flex min-w-0 flex-col gap-3 rounded-control border border-border p-3" data-span-details={span.spanId}>
      <h3 id={headingID} className="break-words text-base font-semibold text-ink">
        {d.title(span.name)}
      </h3>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-2">
        <Term term={d.kind}>{s.trace.kinds[span.kind] ?? span.kind}</Term>
        <Term term={d.status}>
          <span className={failed ? "font-medium text-danger" : ""} data-span-detail-status={span.status}>
            {d.statuses[span.status]}
          </span>
        </Term>
        <Term term={d.started}>
          <time dateTime={span.startedAt}>{formatTime(span.startedAt, TIME_WITH_SECONDS)}</time>
          <span className="block text-xs text-ink-muted">{d.after(s.trace.duration(offset))}</span>
        </Term>
        <Term term={d.took}>{s.trace.duration(span.durationMs)}</Term>
        <Term term={d.sentBy}>{span.source === "otlp" ? d.sender(span.sender?.name ?? span.service, span.service) : d.runner}</Term>
        {span.model && <Term term={d.model}>{span.model}</Term>}
        {span.tokens && <Term term={d.tokens}>{d.tokenCounts(span.tokens.prompt, span.tokens.completion, span.tokens.total)}</Term>}
      </dl>
      {span.statusMessage && (
        <div className="flex flex-col gap-1">
          <h4 className={label}>{d.message}</h4>
          <Pre text={span.statusMessage} data="status-message" />
        </div>
      )}
      <Events events={span.events} />
      <SpanLinks links={span.links} />
      <Value title={d.input} name="input" preview={span.input} whole={wholeValue("input")} reading={whole.isFetching} error={error} textKept={textKept} />
      <Value title={d.output} name="output" preview={span.output} whole={wholeValue("output")} reading={whole.isFetching} error={error} textKept={textKept} />
      <div className="flex flex-col gap-1">
        <h4 className={label}>{d.attributes}</h4>
        <Pairs pairs={span.attributes} name="attributes" />
      </div>
      {whole.data && Object.keys(whole.data.content).length > 0 && (
        <div className="flex flex-col gap-1">
          <h4 className={label}>{d.text}</h4>
          <Pairs pairs={whole.data.content} name="content" />
        </div>
      )}
    </section>
  );
}
