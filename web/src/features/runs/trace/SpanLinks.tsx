import { Link } from "@tanstack/react-router";
import type { SpanLink } from "@/api/traces";
import { s } from "@/strings";
import { Pairs } from "./SpanText";

/** Where one link leads: the run's page or the own trace's page when the reader may read it, else its ids alone. */
function Target({ link }: { link: SpanLink }) {
  const t = s.trace.details.links;
  const className = "text-sm font-medium text-accent underline-offset-2 hover:underline";
  if (link.runId) {
    return (
      <Link to="/runs/$runID" params={{ runID: link.runId }} search={{ span: link.spanId }} className={className} data-span-link-target="run">
        {t.inRun}
      </Link>
    );
  }
  if (link.sender) {
    return (
      <Link
        to="/traces/$traceID"
        params={{ traceID: link.traceId }}
        search={{ sender: link.sender, span: link.spanId }}
        className={className}
        data-span-link-target="trace"
      >
        {t.inTrace}
      </Link>
    );
  }
  return (
    <span className="text-sm text-ink-muted" data-span-link-target="none">
      {t.unreadable}
    </span>
  );
}

/** The spans a span names as related (#262), each with its ids and its attributes without text. */
export function SpanLinks({ links }: { links: SpanLink[] }) {
  const t = s.trace.details.links;
  if (links.length === 0) return null;
  return (
    <div className="flex flex-col gap-1">
      <h4 className="text-xs font-medium text-ink-muted">{t.title}</h4>
      <ul className="flex flex-col gap-2" data-span-links>
        {links.map((link, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: one span may be linked twice, and links never move.
          <li key={`${i}-${link.traceId}-${link.spanId}`} className="flex flex-col gap-1" data-span-link={link.spanId}>
            <Target link={link} />
            <span className="break-all font-mono text-2xs text-ink-muted">{t.ids(link.traceId, link.spanId)}</span>
            {Object.keys(link.attributes).length > 0 && <Pairs pairs={link.attributes} name="link-attributes" />}
          </li>
        ))}
      </ul>
    </div>
  );
}
