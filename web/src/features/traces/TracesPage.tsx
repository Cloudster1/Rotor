import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { meQuery } from "@/api/auth";
import { ownTracesQuery, type TraceSummary } from "@/api/traces";
import { OWN_TRACES_PAGE_SIZE } from "@/config";
import { Pager, shownOf, Tag, tableFrame, td, th } from "@/features/access/ui";
import { link } from "@/features/settings/styles";
import { PageIntro } from "@/features/settings/ui";
import { EmptyState, PageHeader } from "@/features/shell/EmptyState";
import { Icon } from "@/features/shell/icons";
import { formatDateTime } from "@/lib/dates";
import { s } from "@/strings";
import { SendSpans } from "./SendSpans";

/** The search the Traces page keeps its place in, so a page of the list can be linked and reloaded. */
export interface TracesSearch {
  mine?: true;
  offset?: number;
}

/** Reads the list's place from the address, dropping anything that is not one. */
export function tracesSearch(search: Record<string, unknown>): TracesSearch {
  const offset = Number(search.offset);
  return {
    mine: search.mine === true || search.mine === "true" ? true : undefined,
    offset: Number.isInteger(offset) && offset > 0 ? offset : undefined,
  };
}

function Status({ trace }: { trace: TraceSummary }) {
  const t = s.traces;
  return (
    <span className="flex flex-wrap items-center gap-2">
      {trace.failed ? (
        <span className="flex items-center gap-1 font-medium text-danger" data-trace-failed>
          <Icon.Warning className="shrink-0" />
          {t.failed}
        </span>
      ) : (
        <span className="text-ink-muted">{t.ok}</span>
      )}
      {trace.cut && <Tag data-trace-cut="">{t.cut}</Tag>}
    </span>
  );
}

function TraceRow({ trace, everyone }: { trace: TraceSummary; everyone: boolean }) {
  const t = s.traces;
  const name = trace.rootName || t.unnamed;
  return (
    <tr className="border-t border-border" data-trace={trace.traceId}>
      <td className={td}>
        <Link
          to="/traces/$traceID"
          params={{ traceID: trace.traceId }}
          search={{ sender: trace.sender.id }}
          className="font-medium break-all text-accent hover:underline"
        >
          {name}
        </Link>
        <span className="block font-mono text-2xs text-ink-muted">{trace.traceId}</span>
      </td>
      {everyone && (
        <td className={td} data-trace-sender>
          {trace.sender.name}
        </td>
      )}
      <td className={`${td} break-all`}>{trace.service}</td>
      <td className={`${td} tabular-nums`}>{trace.spans}</td>
      <td className={`${td} whitespace-nowrap`}>
        <time dateTime={trace.startedAt}>{formatDateTime(trace.startedAt)}</time>
      </td>
      <td className={td}>
        <Status trace={trace} />
      </td>
    </tr>
  );
}

/** The traces senders posted of no run (#257): the person's own, and every sender's for an administrator. */
export function TracesPage({ search }: { search: TracesSearch }) {
  const t = s.traces;
  const navigate = useNavigate();
  const me = useQuery(meQuery).data;
  const admin = me?.role === "admin";
  // Without the filter an administrator reads every sender's, as the list answers them; anyone else reads their own.
  const sender = admin && search.mine ? me.user.id : undefined;
  const everyone = admin && !search.mine;
  const list = useQuery({ ...ownTracesQuery({ sender, offset: search.offset ?? 0, limit: OWN_TRACES_PAGE_SIZE }), enabled: me !== undefined });
  const data = list.data;
  const go = (change: TracesSearch) => void navigate({ to: "/traces", search: { ...search, ...change } });
  const Glyph = Icon.Trace;
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={s.places.traces} />
      <PageIntro>{admin ? t.introAdmin : t.intro}</PageIntro>
      {/* Spans of runs are searched here too, since a run's trace has no list of its own. */}
      <Link to="/traces/search" className={`${link} self-start`} data-span-search-open>
        {t.search.open}
      </Link>
      {admin && (
        <label className="flex h-9 items-center gap-2 text-sm text-ink">
          <input
            type="checkbox"
            checked={search.mine === true}
            onChange={(e) => go({ mine: e.target.checked || undefined, offset: undefined })}
            data-filter-mine
          />
          {t.mine}
        </label>
      )}
      {list.isPending && <p className="text-sm text-ink-muted">{t.loading}</p>}
      {list.isError && (
        <p role="alert" className="text-sm text-danger">
          {t.unreachable}
        </p>
      )}
      {data && data.total === 0 && (
        <EmptyState
          icon={<Glyph />}
          title={search.mine ? t.noneOfMineTitle : t.emptyTitle}
          description={search.mine ? t.noneOfMine : t.emptyBody}
          action={<SendSpans />}
        />
      )}
      {data && data.total > 0 && (
        <div className="flex flex-col gap-3">
          <p className="text-sm text-ink-muted" data-trace-count>
            {shownOf(data.total, data.offset, data.traces.length, t.count)}
          </p>
          <div className={tableFrame}>
            <table className="w-full text-left text-sm text-ink" data-traces>
              <caption className="sr-only">{t.label}</caption>
              <thead className="text-xs text-ink-muted">
                <tr>
                  <th scope="col" className={th}>
                    {t.columns.trace}
                  </th>
                  {everyone && (
                    <th scope="col" className={th}>
                      {t.columns.sender}
                    </th>
                  )}
                  <th scope="col" className={th}>
                    {t.columns.service}
                  </th>
                  <th scope="col" className={th}>
                    {t.columns.spans}
                  </th>
                  <th scope="col" className={th}>
                    {t.columns.started}
                  </th>
                  <th scope="col" className={th}>
                    {t.columns.status}
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.traces.map((trace) => (
                  <TraceRow key={`${trace.sender.id}/${trace.traceId}`} trace={trace} everyone={everyone} />
                ))}
              </tbody>
            </table>
          </div>
          <Pager
            label={t.pages}
            total={data.total}
            offset={data.offset}
            pageSize={OWN_TRACES_PAGE_SIZE}
            onChange={(offset) => go({ offset: offset || undefined })}
          />
        </div>
      )}
    </div>
  );
}
