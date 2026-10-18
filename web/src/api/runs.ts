import { queryOptions } from "@tanstack/react-query";
import { API_BASE, RUN_POLL_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

/** A run as its viewers see it. */
export type Run = components["schemas"]["RunResponse"];
export type RunStatus = Run["status"];
/** The pinned version's nodes and edges, which one run carries for its page. */
export type RunGraph = NonNullable<Run["graph"]>;

/** Every state a run can be in, in the order it moves through them. */
export const RUN_STATUSES: RunStatus[] = ["queued", "starting", "running", "waiting", "succeeded", "failed", "cancelled", "expired"];

/** One event of a run, as its stream sends it (docs/api-contract-m2.md). */
export interface RunEvent {
  seq: number;
  type: string;
  node: string;
  payload: Record<string, unknown>;
  at: string;
}

/** What narrows a list of runs; nothing set lists every run the person may see. */
export interface RunFilter {
  workflow?: string;
  status?: RunStatus;
  mine?: boolean;
}

export const runsQueryKey = ["runs"] as const;

/** One run, asked again until it ends: its state moves without an event while it queues and starts. */
export const runQuery = (id: string) =>
  queryOptions({
    queryKey: [...runsQueryKey, id],
    queryFn: async (): Promise<Run> => {
      const { data } = await api.GET("/runs/{runID}", { params: { path: { runID: id } } });
      return data as Run;
    },
    refetchInterval: (query) => (query.state.data?.ended ? false : RUN_POLL_MS),
  });

/** The runs the person may see, newest first, asked again while any of them has not ended. */
export const runsQuery = (filter: RunFilter) =>
  queryOptions({
    queryKey: [...runsQueryKey, "list", filter],
    queryFn: async (): Promise<Run[]> => {
      const { data } = await api.GET("/runs", {
        params: { query: { workflow: filter.workflow, status: filter.status ? [filter.status] : undefined, mine: filter.mine ? "true" : undefined } },
      });
      return (data?.runs ?? []) as Run[];
    },
    refetchInterval: (query) => (query.state.data?.some((r) => !r.ended) ? RUN_POLL_MS : false),
  });

/** Where a run's events stream from; the browser's EventSource resumes after the last event it saw. */
export function runEventsURL(id: string): string {
  return `${API_BASE}/runs/${encodeURIComponent(id)}/events`;
}

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
