import { queryOptions } from "@tanstack/react-query";
import { RUN_POLL_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

/** A run as its viewers see it. */
export type Run = components["schemas"]["RunResponse"];
export type RunStatus = Run["status"];

export const runsQueryKey = ["runs"] as const;

/** One run, asked again until it ends; #24 streams its events instead. */
export const runQuery = (id: string) =>
  queryOptions({
    queryKey: [...runsQueryKey, id],
    queryFn: async (): Promise<Run> => {
      const { data } = await api.GET("/runs/{runID}", { params: { path: { runID: id } } });
      return data as Run;
    },
    refetchInterval: (query) => (query.state.data?.ended ? false : RUN_POLL_MS),
  });

/** Starts a run as the signed-in person; the key makes a retried click start nothing twice. */
export async function startRun(workflowID: string, inputs: Record<string, unknown>, idempotencyKey: string): Promise<Run> {
  const { data } = await api.POST("/workflows/{workflowID}/runs", {
    params: { path: { workflowID }, header: { "Idempotency-Key": idempotencyKey } },
    body: { inputs },
  });
  return data as Run;
}

/** Cancels a run that has not ended. */
export async function cancelRun(id: string): Promise<Run> {
  const { data } = await api.POST("/runs/{runID}/cancel", { params: { path: { runID: id } } });
  return data as Run;
}
