import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** How one workflow's runs are traced (#259): text kept, destinations that get them and the share sent. */
export type WorkflowTracingResponse = components["schemas"]["WorkflowTracingResponse"];
export type WorkflowTracingRequest = components["schemas"]["WorkflowTracingRequest"];
export type TracingDestination = components["schemas"]["TracingDestination"];

export const workflowTracingQueryKey = (workflowID: string) => ["workflows", workflowID, "tracing"] as const;

export const workflowTracingQuery = (workflowID: string) =>
  queryOptions({
    queryKey: workflowTracingQueryKey(workflowID),
    queryFn: async (): Promise<WorkflowTracingResponse> => {
      const { data } = await api.GET("/workflows/{workflowID}/settings/tracing", { params: { path: { workflowID } } });
      return data as WorkflowTracingResponse;
    },
  });

export async function setWorkflowTracing(workflowID: string, body: WorkflowTracingRequest): Promise<WorkflowTracingResponse> {
  const { data } = await api.PUT("/workflows/{workflowID}/settings/tracing", { params: { path: { workflowID } }, body });
  return data as WorkflowTracingResponse;
}
