import { Link } from "@tanstack/react-router";
import type { SpanHit } from "@/api/spans";
import { tableFrame, td, th } from "@/features/access/ui";
import { Icon } from "@/features/shell/icons";
import { formatDateTime } from "@/lib/dates";
import { s } from "@/strings";

const linkStyle = "font-medium break-all text-accent hover:underline";

/** The span's name, opening the run's trace or the sender's own with the span chosen. */
function SpanLink({ hit }: { hit: SpanHit }) {
  if (hit.runId) {
    return (
      <Link to="/runs/$runID" params={{ runID: hit.runId }} search={{ span: hit.spanId }} className={linkStyle} data-span-hit-link>
        {hit.name}
      </Link>
    );
  }
  return (
    <Link to="/traces/$traceID" params={{ traceID: hit.traceId }} search={{ sender: hit.owner?.id, span: hit.spanId }} className={linkStyle} data-span-hit-link>
      {hit.name}
    </Link>
  );
}

function Status({ hit }: { hit: SpanHit }) {
  if (hit.status !== "error") return <span className="text-ink-muted">{s.traces.search.ok}</span>;
  return (
    <span className="flex flex-col gap-1">
      <span className="flex items-center gap-1 font-medium text-danger" data-span-hit-failed>
        <Icon.Warning className="shrink-0" />
        {s.trace.failed}
      </span>
      {hit.statusMessage && <span className="text-xs break-words text-ink-muted">{hit.statusMessage}</span>}
    </span>
  );
}

function HitRow({ hit }: { hit: SpanHit }) {
  const t = s.traces.search;
  return (
    <tr className="border-t border-border" data-span-hit={hit.spanId}>
      <td className={td}>
        <SpanLink hit={hit} />
        <span className="block text-xs text-ink-muted">{s.trace.kinds[hit.kind]}</span>
      </td>
      <td className={td}>
        <Status hit={hit} />
      </td>
      <td className={`${td} break-words`} data-span-hit-where>
        {hit.runId ? t.runOf(hit.workflow?.name ?? "") : t.ownTrace(hit.owner?.name ?? "")}
      </td>
      <td className={`${td} break-all`}>{hit.service}</td>
      <td className={`${td} break-all`}>{hit.model}</td>
      <td className={`${td} whitespace-nowrap`}>
        <time dateTime={hit.startedAt}>{formatDateTime(hit.startedAt)}</time>
      </td>
      <td className={`${td} text-right tabular-nums`}>{s.trace.duration(hit.durationMs)}</td>
    </tr>
  );
}

/** The spans a search found, newest first; names and messages are shown as text, never as markup or links. */
export function SpanHits({ hits }: { hits: SpanHit[] }) {
  const c = s.traces.search.columns;
  return (
    <div className={tableFrame}>
      <table className="w-full text-left text-sm text-ink" data-span-hits>
        <caption className="sr-only">{s.traces.search.label}</caption>
        <thead className="text-xs text-ink-muted">
          <tr>
            {[c.span, c.status, c.where, c.service, c.model, c.started, c.took].map((head) => (
              <th key={head} scope="col" className={th}>
                {head}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {hits.map((hit) => (
            <HitRow key={`${hit.traceId}/${hit.runId ?? hit.owner?.id ?? ""}/${hit.spanId}`} hit={hit} />
          ))}
        </tbody>
      </table>
    </div>
  );
}
