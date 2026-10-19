import type { ApprovalStatus } from "@/api/approvals";
import { s } from "@/strings";

/** How each state reads at a glance, in the colours the run states use. */
const tone: Record<ApprovalStatus, string> = {
  pending: "bg-warning-subtle text-ink",
  approved: "bg-success-subtle text-ink",
  rejected: "bg-danger-subtle text-ink",
  expired: "bg-surface-raised text-ink",
  cancelled: "bg-surface-raised text-ink",
};

/** An approval's state as a badge. */
export function ApprovalStatusBadge({ status }: { status: ApprovalStatus }) {
  return (
    <span className={`rounded-control px-2 py-0.5 text-xs font-medium ${tone[status]}`} data-approval-status={status}>
      {s.approvals.status[status]}
    </span>
  );
}
