import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { type Approval, approvalQuery } from "@/api/approvals";
import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";
import { ApprovalDecision } from "./ApprovalDecision";
import { ApprovalPreview } from "./ApprovalPreview";
import { ApprovalStatusBadge } from "./ApprovalStatusBadge";
import { deciders, when } from "./ApprovalsPage";

const pre = "overflow-x-auto whitespace-pre-wrap break-words rounded-control border border-border bg-surface-sunken p-2 font-mono text-xs text-ink";

/** An approval nobody decides any more, or one the person may only look at: how it ended, or who decides it. */
function Settled({ approval }: { approval: Approval }) {
  const t = s.approvals.page;
  const d = approval.decision;
  if (d) {
    return (
      <section className="flex max-w-2xl flex-col gap-2" data-approval-decided>
        <p className="text-sm text-ink">{t.decided(s.approvals.status[d.decision === "approve" ? "approved" : "rejected"], d.by.name, when(d.at))}</p>
        {d.comment && <blockquote className="border-l-2 border-border-strong pl-3 text-sm text-ink">{d.comment}</blockquote>}
        {d.decision === "approve" && d.values !== null && d.values !== undefined && approval.form && (
          <>
            <p className="text-xs font-medium text-ink-muted">{t.values}</p>
            <pre className={pre}>{JSON.stringify(d.values, null, 2)}</pre>
          </>
        )}
      </section>
    );
  }
  return <p className="text-sm text-ink-muted">{approval.status === "pending" ? t.notYours : t.ended(s.approvals.status[approval.status])}</p>;
}

/** One approval: what the run asks, what it is about to write, and deciding it when it is the person's to decide. */
export function ApprovalPage({ approvalID }: { approvalID: string }) {
  const approval = useQuery(approvalQuery(approvalID));
  const t = s.approvals.page;
  if (approval.isError) {
    return (
      <div>
        <PageHeader title={s.places.approvals} />
        <p role="alert" className="text-sm text-danger">
          {t.notFound}
        </p>
      </div>
    );
  }
  if (!approval.data) return <p className="text-sm text-ink-muted">{t.loading}</p>;
  const a = approval.data;
  const open = a.status === "pending" && a.canDecide;
  return (
    <div className="flex max-w-4xl flex-col gap-6">
      <PageHeader title={s.approvals.title(a.run.workflow.name)} />
      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-3 text-sm text-ink-muted">
          <ApprovalStatusBadge status={a.status} />
          <span>{s.approvals.startedBy(a.run.startedBy.name)}</span>
          <span>{deciders(a)}</span>
          {a.status === "pending" && <span>{s.approvals.expires(when(a.expiresAt))}</span>}
          <Link to="/runs/$runID" params={{ runID: a.run.id }} className="text-accent underline-offset-2 hover:underline" data-approval-run>
            {t.openRun}
          </Link>
        </div>
        <p className="text-xs font-medium text-ink-muted">{t.asks(a.node)}</p>
        <p className="text-base text-ink" data-approval-message>
          {a.message}
        </p>
      </div>
      <ApprovalPreview writes={a.preview} />
      {open ? <ApprovalDecision key={a.id} approval={a} /> : <Settled approval={a} />}
    </div>
  );
}

/** An approval by the id in the address, as a link from the inbox, a run or a message opens it. */
export function ApprovalRoute() {
  const { approvalID } = useParams({ from: "/shell/approvals/$approvalID" });
  return <ApprovalPage approvalID={approvalID} />;
}
