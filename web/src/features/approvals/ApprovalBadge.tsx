import { useQuery } from "@tanstack/react-query";
import { approvalCountQuery } from "@/api/approvals";
import { s } from "@/strings";

/** How many approvals wait for the person, beside the sidebar's Approvals; nothing when none do. */
export function ApprovalBadge() {
  const count = useQuery(approvalCountQuery());
  const n = count.data ?? 0;
  if (n <= 0) return null;
  return (
    <>
      <span
        aria-hidden="true"
        className="ml-auto min-w-5 rounded-full bg-accent px-1.5 text-center text-xs font-medium leading-5 text-on-accent"
        data-approval-badge={n}
      >
        {n}
      </span>
      <span className="sr-only">{s.approvals.badge(n)}</span>
    </>
  );
}
