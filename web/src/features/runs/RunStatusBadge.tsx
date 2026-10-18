import type { RunStatus } from "@/api/runs";
import { s } from "@/strings";

/** How each state reads at a glance: settled ones in their colour, moving ones neutral. */
const tone: Record<RunStatus, string> = {
  queued: "bg-surface-raised text-ink",
  starting: "bg-surface-raised text-ink",
  running: "bg-accent-subtle text-ink",
  waiting: "bg-warning-subtle text-ink",
  succeeded: "bg-success-subtle text-ink",
  failed: "bg-danger-subtle text-ink",
  cancelled: "bg-surface-raised text-ink",
  expired: "bg-surface-raised text-ink",
};

/** A run's state as a badge. */
export function RunStatusBadge({ status }: { status: RunStatus }) {
  return (
    <span className={`rounded-control px-2 py-0.5 text-xs font-medium ${tone[status]}`} data-run-status={status}>
      {s.runs.status[status]}
    </span>
  );
}
