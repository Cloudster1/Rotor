import type { SpanEvent, SpanValue } from "@/api/traces";
import { formatTime } from "@/lib/dates";
import { s } from "@/strings";
import { readable } from "./tree";

// Span text is whatever a run, a model or a sender wrote: React writes it as text, so nothing in it is markup, and no
// address in it becomes a link (threat model, M18).
const pre = "max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-control border border-border bg-surface-sunken p-2 font-mono text-xs text-ink";
const label = "text-xs font-medium text-ink-muted";
const EXCEPTION = "exception";
const EXCEPTION_KEYS = ["exception.type", "exception.message", "exception.stacktrace"];
/** A time of day to the second, since a span's steps are seconds apart. */
export const TIME_WITH_SECONDS: Intl.DateTimeFormatOptions = { timeStyle: "medium" };

/** A value as text; it scrolls past a screenful, so it takes focus to be scrolled by keyboard. */
export function Pre({ text, data }: { text: string; data?: string }) {
  return (
    <pre className={pre} tabIndex={0} data-span-text={data}>
      {text}
    </pre>
  );
}

/** Keys and values as a list of terms; a value may be long, so it wraps under its key. */
export function Pairs({ pairs, name }: { pairs: Record<string, string>; name: string }) {
  const keys = Object.keys(pairs).sort();
  if (keys.length === 0) return <p className="text-sm text-ink-muted">{s.trace.details.noAttributes}</p>;
  return (
    <dl className="flex flex-col gap-1 text-xs" data-span-pairs={name}>
      {keys.map((key) => (
        <div key={key} className="flex flex-col">
          <dt className="font-mono text-ink-muted">{key}</dt>
          <dd className="whitespace-pre-wrap break-words font-mono text-ink">{pairs[key]}</dd>
        </div>
      ))}
    </dl>
  );
}

interface ValueProps {
  title: string;
  name: "input" | "output";
  preview: SpanValue | null;
  /** The whole value from the span route: undefined until it is read, or when it is not asked for. */
  whole: SpanValue | null | undefined;
  reading: boolean;
  error: string | null;
  textKept: boolean;
}

/** An input or output: the tree's start of it at once, the whole once the span route answers. */
export function Value({ title, name, preview, whole, reading, error, textKept }: ValueProps) {
  const d = s.trace.details;
  const value = whole === undefined ? preview : whole;
  let note: string | null = null;
  if (value === null) note = textKept ? d.nothing : d.notKept;
  else if (whole === undefined && preview?.cut) note = reading ? d.reading : error ? d.readFailed(error) : d.preview;
  else if (value.cut) note = d.cut;
  return (
    <div className="flex flex-col gap-1" data-span-value={name}>
      <h4 className={label}>{title}</h4>
      {value && <Pre text={readable(value.value, value.mimeType)} data={name} />}
      {note && <p className="text-xs text-ink-muted">{note}</p>}
    </div>
  );
}

/** A span's events, an exception with its type, message and stack set apart, since that is where a run failed. */
export function Events({ events }: { events: SpanEvent[] }) {
  const d = s.trace.details;
  if (events.length === 0) return null;
  return (
    <div className="flex flex-col gap-2" data-span-events>
      <h4 className={label}>{d.events}</h4>
      <ol className="flex flex-col gap-3">
        {events.map((event, i) => {
          const exception = event.name === EXCEPTION;
          const rest = exception ? Object.fromEntries(Object.entries(event.attributes).filter(([key]) => !EXCEPTION_KEYS.includes(key))) : event.attributes;
          const type = event.attributes["exception.type"];
          const message = event.attributes["exception.message"];
          const stack = event.attributes["exception.stacktrace"];
          return (
            // Events carry no id and never change once kept, so their place is who they are.
            <li key={`${i}-${event.at}`} className="flex flex-col gap-1" data-span-event={event.name}>
              <p className={`text-sm ${exception ? "font-medium text-danger" : "text-ink"}`}>
                <time dateTime={event.at} className="mr-2 text-xs font-normal text-ink-muted">
                  {formatTime(event.at, TIME_WITH_SECONDS)}
                </time>
                {exception ? `${d.exception}${type ? `: ${type}` : ""}` : event.name}
              </p>
              {message && <Pre text={message} data="exception-message" />}
              {stack && (
                <>
                  <p className={label}>{d.stack}</p>
                  <Pre text={stack} data="exception-stack" />
                </>
              )}
              {Object.keys(rest).length > 0 && <Pairs pairs={rest} name="event" />}
            </li>
          );
        })}
      </ol>
    </div>
  );
}
