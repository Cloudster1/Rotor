import { createRoute } from "@tanstack/react-router";
import { ApprovalRoute } from "@/features/approvals/ApprovalPage";
import { ApprovalsPage, approvalsSearch } from "@/features/approvals/ApprovalsPage";
import { shellRoute } from "./shell";

export const approvalsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/approvals",
  validateSearch: approvalsSearch,
  component: function ApprovalsRoute() {
    return <ApprovalsPage search={approvalsRoute.useSearch()} />;
  },
});

export const approvalRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/approvals/$approvalID",
  component: ApprovalRoute,
});
