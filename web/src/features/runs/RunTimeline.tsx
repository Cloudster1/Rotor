import { Link } from "@tanstack/react-router";
import { s } from "@/strings";
import { type Entry, linksIn } from "./timeline";

const pre = "overflow-x-auto whitespace-pre-wrap break-words rounded-control border border-border bg-surface-sunken p-2 font-mono text-xs text-ink";

function time(at: string): string {
  return new Date(at).toLocaleTimeString();
}

/** What one entry says, in words, with a tool call's arguments and answer folded away until asked for. */
function Line({ entry, hosts }: { entry: Entry; hosts: readonly string[] }) {
  const t = s.runs.live;
  switch (entry.kind) {
    case "started":
      return entry.repeats ? <p data-visit={entry.visit}>{t.startedVisit(entry.node, entry.type, entry.visit)}</p> : <p>{t.started(entry.node, entry.type)}</p>;
    case "finished":
      return entry.ok ? (
        <p>{t.finished(entry.node)}</p>
      ) : (
        <p className="text-danger">
          {t.failedNode(entry.node)}: {entry.error}
        </p>
      );
    case "tool": {
      const links = linksIn(entry.result, hosts);
      return (
        <details data-tool-call={entry.tool}>
          <summary className={`cursor-pointer ${entry.ok ? "" : "text-danger"}`}>
            {entry.ok ? t.called(entry.tool, entry.binding) : t.callFailed(entry.tool, entry.binding)}
          </summary>
          <div className="mt-2 flex flex-col gap-2">
            <p className="text-xs font-medium text-ink-muted">{t.arguments}</p>
            <pre className={pre}>{entry.args}</pre>
            <p className="text-xs font-medium text-ink-muted">{t.answer}</p>
            <pre className={pre}>{entry.result}</pre>
          </div>
          {links.length > 0 && (
            <ul className="mt-2 flex flex-col gap-1" aria-label={t.links}>
              {links.map((href) => (
                <li key={href}>
                  <a href={href} target="_blank" rel="noreferrer" className="break-all text-accent underline-offset-2 hover:underline" data-tool-link>
                    {href}
                  </a>
                </li>
              ))}
            </ul>
          )}
        </details>
      );
    }
    case "log":
      return (
        <div>
          <p className="text-xs text-ink-muted">{t.printed(entry.node)}</p>
          <pre className={pre}>{entry.text}</pre>
        </div>
      );
    case "llm":
      return (
        <div>
          <p className="text-xs text-ink-muted">{t.modelWrote(entry.node)}</p>
          <p className="whitespace-pre-wrap">{entry.text}</p>
        </div>
      );
    case "usage":
      return <p className="text-xs text-ink-muted">{s.usage.timeline(entry.node, entry.input, entry.output)}</p>;
    case "interrupt":
      return <p className="text-warning">{t.asks(entry.node, entry.message)}</p>;
    case "requested": {
      const a = s.approvals.live;
      return (
        <p className="text-warning">
          {a.requested(entry.node, entry.group ? a.group(entry.group) : a.starter)}{" "}
          {entry.approval && (
            <Link to="/approvals/$approvalID" params={{ approvalID: entry.approval }} className="text-accent underline-offset-2 hover:underline">
              {s.approvals.run.open}
            </Link>
          )}
        </p>
      );
    }
    case "decided": {
      const status = entry.decision === "approve" ? "approved" : entry.decision === "reject" ? "rejected" : undefined;
      return (
        <div>
          <p>{s.approvals.live.decided(status ? s.approvals.status[status] : entry.decision, entry.by)}</p>
          {entry.comment && <p className="text-ink-muted">{entry.comment}</p>}
        </div>
      );
    }
    case "run":
      return (
        <p className="font-medium">
          {t.ended(s.runs.status[entry.status as keyof typeof s.runs.status] ?? entry.status)}
          {entry.reason && ` (${entry.reason})`}
          {entry.message && `: ${entry.message}`}
        </p>
      );
    case "lost":
      return <p className="text-warning">{entry.replaced ? t.replaced : t.lost}</p>;
    case "links": {
      const u = s.untrusted;
      const where = u.linksRemovedWhere[entry.where as keyof typeof u.linksRemovedWhere];
      return (
        <p className="text-warning" data-links-removed>
          {where ? `${where}: ` : ""}
          {u.linksRemoved(entry.count, entry.hosts)}
        </p>
      );
    }
  }
}

/** The run's events in order, in words; it grows as the run goes on. Links go only to hosts, the run's tools. */
export function RunTimeline({ entries, live, hosts = [] }: { entries: Entry[]; live: boolean; hosts?: readonly string[] }) {
  return (
    <section aria-labelledby="run-timeline-title" className="flex min-w-0 flex-col gap-2" data-run-timeline>
      <h2 id="run-timeline-title" className="text-lg font-semibold text-ink">
        {s.runs.live.timeline}
      </h2>
      {entries.length === 0 && <p className="text-sm text-ink-muted">{live ? s.runs.live.nothingYet : s.runs.live.nothing}</p>}
      <ol className="flex flex-col gap-3 text-sm text-ink" aria-live="polite">
        {entries.map((entry) => (
          <li key={entry.key} className="flex gap-3" data-entry={entry.kind}>
            <time dateTime={entry.at} className="w-20 shrink-0 text-xs text-ink-muted">
              {time(entry.at)}
            </time>
            <div className="min-w-0 flex-1">
              <Line entry={entry} hosts={hosts} />
            </div>
          </li>
        ))}
      </ol>
    </section>
  );
}
