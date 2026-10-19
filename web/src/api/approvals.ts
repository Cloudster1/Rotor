import { queryOptions } from "@tanstack/react-query";
import { APPROVALS_POLL_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

/** An approval in the inbox, and one with its preview, form and decision (docs/api-contract-m5.md). */
export type ApprovalSummary = components["schemas"]["ApprovalSummary"];
export type Approval = components["schemas"]["ApprovalResponse"];
export type ApprovalStatus = Approval["status"];
export type PreviewWrite = components["schemas"]["PreviewWrite"];
export type Decision = components["schemas"]["DecisionRequest"];

/** Which approvals the inbox lists: those waiting for the person, or every one they may see. */
export type ApprovalsShow = "waiting" | "all";

export const approvalsQueryKey = ["approvals"] as const;

/** The inbox: pending approvals the person may decide, or all they may see, newest first. */
export const approvalsQuery = (show: ApprovalsShow) =>
  queryOptions({
    queryKey: [...approvalsQueryKey, "list", show],
    queryFn: async (): Promise<ApprovalSummary[]> => {
      const query = show === "waiting" ? { waitingOnMe: "true" } : { status: "all" };
      const { data } = await api.GET("/approvals", { params: { query } });
      return data?.approvals ?? [];
    },
  });

/** How many approvals wait for the person, for the sidebar's badge; asked again now and then. */
export const approvalCountQuery = () =>
  queryOptions({
    queryKey: [...approvalsQueryKey, "count"],
    queryFn: async (): Promise<number> => {
      const { data } = await api.GET("/approvals/count");
      return data?.waitingOnMe ?? 0;
    },
    refetchInterval: APPROVALS_POLL_MS,
  });

/** One approval, with what the run is about to write. */
export const approvalQuery = (id: string) =>
  queryOptions({
    queryKey: [...approvalsQueryKey, id],
    queryFn: async (): Promise<Approval> => {
      const { data } = await api.GET("/approvals/{approvalID}", { params: { path: { approvalID: id } } });
      return data as Approval;
    },
  });

/** Approves or rejects; the answer is the approval as decided. */
export async function decideApproval(id: string, decision: Decision): Promise<Approval> {
  const { data } = await api.POST("/approvals/{approvalID}/decision", { params: { path: { approvalID: id } }, body: decision });
  return data as Approval;
}
