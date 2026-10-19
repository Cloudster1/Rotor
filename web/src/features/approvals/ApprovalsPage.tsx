import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { type ApprovalSummary, type ApprovalsShow, approvalsQuery } from "@/api/approvals";
import { EmptyState, PageHeader } from "@/features/shell/EmptyState";
import { Icon } from "@/features/shell/icons";
import { s } from "@/strings";
import { ApprovalStatusBadge } from "./ApprovalStatusBadge";

/** The search the inbox keeps its choice in, so either list can be linked and reloaded. */
export interface ApprovalsSearch {
  show?: "all";
}

/** Reads the choice from the address; anything but "all" is the inbox of what waits for the person. */
export function approvalsSearch(search: Record<string, unknown>): ApprovalsSearch {
  return search.show === "all" ? { show: "all" } : {};
}

export function when(at: string | null): string {
  return at ? new Date(at).toLocaleString() : "";
}

/** Who decides an approval, in words: the group the node names, or the person who started the run. */
export function deciders(a: ApprovalSummary): string {
  return a.approvers.group ? s.approvals.byGroup(a.approvers.group) : s.approvals.byStarter(a.run.startedBy.name);
}

const tab = "h-8 rounded-control px-3 text-sm font-medium";
const tabOn = `${tab} bg-accent-subtle text-accent`;
const tabOff = `${tab} text-ink-muted hover:bg-surface-raised hover:text-ink`;

/** The approvals inbox: what waits for the person to decide, or every approval they may see. */
export function ApprovalsPage({ search }: { search: ApprovalsSearch }) {
  const navigate = useNavigate();
  const show: ApprovalsShow = search.show === "all" ? "all" : "waiting";
  const approvals = useQuery(approvalsQuery(show));
  const Glyph = Icon.Approval;
  const choices: [ApprovalsShow, string][] = [
    ["waiting", s.approvals.waiting],
    ["all", s.approvals.all],
  ];
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={s.places.approvals} />
      <div role="group" aria-label={s.approvals.show} className="flex gap-2">
        {choices.map(([value, label]) => (
          <button
            key={value}
            type="button"
            className={show === value ? tabOn : tabOff}
            aria-pressed={show === value}
            onClick={() => void navigate({ to: "/approvals", search: value === "all" ? { show: "all" } : {} })}
            data-show={value}
          >
            {label}
          </button>
        ))}
      </div>
      {approvals.isPending && <p className="text-sm text-ink-muted">{s.approvals.loading}</p>}
      {approvals.isError && (
        <p role="alert" className="text-sm text-danger">
          {s.approvals.unreachable}
        </p>
      )}
      {approvals.data && approvals.data.length === 0 && (
        <EmptyState
          icon={<Glyph />}
          title={show === "waiting" ? s.approvals.emptyTitle : s.approvals.allEmptyTitle}
          description={show === "waiting" ? s.approvals.emptyBody : s.approvals.allEmptyBody}
        />
      )}
      {approvals.data && approvals.data.length > 0 && (
        <ul className="flex max-w-4xl flex-col gap-3" aria-label={s.approvals.label} data-approvals>
          {approvals.data.map((a) => (
            <li key={a.id} className="rounded-overlay border border-border bg-surface p-4 shadow-1" data-approval={a.id}>
              <div className="flex flex-wrap items-baseline justify-between gap-2">
                <Link to="/approvals/$approvalID" params={{ approvalID: a.id }} className="font-medium text-accent hover:underline">
                  {s.approvals.title(a.run.workflow.name)}
                </Link>
                <ApprovalStatusBadge status={a.status} />
              </div>
              <p className="mt-1 text-sm text-ink">{a.message}</p>
              <p className="mt-1 text-xs text-ink-muted">
                {s.approvals.startedBy(a.run.startedBy.name)} . {deciders(a)} . {s.approvals.requested(when(a.requestedAt))}
                {a.status === "pending" && ` . ${s.approvals.expires(when(a.expiresAt))}`}
              </p>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
