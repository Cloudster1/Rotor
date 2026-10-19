import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** A workflow in a list, and with its current version. */
export type WorkflowSummary = components["schemas"]["WorkflowSummary"];
export type WorkflowResponse = components["schemas"]["WorkflowResponse"];
export type Version = components["schemas"]["VersionResponse"];
export type VersionSummary = components["schemas"]["VersionSummary"];
export type Diff = components["schemas"]["DiffResponse"];
export type Problem = components["schemas"]["Problem"];
export type Sharing = components["schemas"]["SharingRequest"]["sharing"];
export type Catalog = components["schemas"]["CatalogResponse"];
export type CatalogTool = components["schemas"]["CatalogTool"];

export const workflowsQueryKey = ["workflows"] as const;

export const workflowsQuery = (archived: boolean) =>
  queryOptions({
    queryKey: [...workflowsQueryKey, { archived }],
    queryFn: async (): Promise<WorkflowSummary[]> => {
      const { data } = await api.GET("/workflows", { params: { query: { archived: archived ? "true" : undefined } } });
      return (data?.workflows ?? []) as WorkflowSummary[];
    },
  });

export const workflowQuery = (id: string) =>
  queryOptions({
    queryKey: [...workflowsQueryKey, id],
    queryFn: async (): Promise<WorkflowResponse> => {
      const { data } = await api.GET("/workflows/{workflowID}", { params: { path: { workflowID: id } } });
      return data as WorkflowResponse;
    },
  });

export const versionsQuery = (id: string) =>
  queryOptions({
    queryKey: [...workflowsQueryKey, id, "versions"],
    queryFn: async (): Promise<VersionSummary[]> => {
      const { data } = await api.GET("/workflows/{workflowID}/versions", { params: { path: { workflowID: id } } });
      return (data?.versions ?? []) as VersionSummary[];
    },
  });

export const diffQuery = (id: string, from: number, to: number) =>
  queryOptions({
    queryKey: [...workflowsQueryKey, id, "diff", from, to],
    queryFn: async (): Promise<Diff> => {
      const { data } = await api.GET("/workflows/{workflowID}/diff", {
        params: { path: { workflowID: id }, query: { from: String(from), to: String(to) } },
      });
      return data as Diff;
    },
  });

/** One version's YAML, to open in the editor. */
export async function fetchVersion(id: string, number: number): Promise<Version> {
  const { data } = await api.GET("/workflows/{workflowID}/versions/{versionNumber}", {
    params: { path: { workflowID: id, versionNumber: number } },
  });
  return data as Version;
}

/** The workflow schema, for the editor's completion and descriptions; it changes with Rotor's release only. */
export const schemaQuery = queryOptions({
  queryKey: ["schemas", "workflow.v1"],
  queryFn: async (): Promise<Record<string, unknown>> => {
    const { data } = await api.GET("/schemas/workflow.v1.json");
    return data as Record<string, unknown>;
  },
  staleTime: Number.POSITIVE_INFINITY,
});

/** A tool's MCP tools as the signed-in person's connection sees them. */
export const catalogQuery = (toolID: string) =>
  queryOptions({
    queryKey: ["catalog", toolID],
    queryFn: async (): Promise<Catalog> => {
      const { data } = await api.GET("/tools/{toolID}/catalog", { params: { path: { toolID } } });
      return data as Catalog;
    },
    retry: false,
  });

/** What checking a workflow found: problems stop a save, warnings do not (#40). */
export type Checked = { problems: Problem[]; warnings: Problem[] };

/** Checks YAML without saving it. */
export async function validateWorkflow(specYAML: string): Promise<Checked> {
  const { data } = await api.POST("/workflows/validate", { body: { specYAML } });
  return { problems: (data?.problems ?? []) as Problem[], warnings: (data?.warnings ?? []) as Problem[] };
}

export async function createWorkflow(specYAML: string, comment: string): Promise<WorkflowResponse> {
  const { data } = await api.POST("/workflows", { body: { specYAML, comment } });
  return data as WorkflowResponse;
}

export async function saveWorkflow(id: string, specYAML: string, comment: string, baseVersion: number): Promise<WorkflowResponse> {
  const { data } = await api.PUT("/workflows/{workflowID}", { params: { path: { workflowID: id } }, body: { specYAML, comment, baseVersion } });
  return data as WorkflowResponse;
}

export async function shareWorkflow(id: string, sharing: Sharing, groups: string[]): Promise<WorkflowResponse> {
  const { data } = await api.PUT("/workflows/{workflowID}/sharing", { params: { path: { workflowID: id } }, body: { sharing, groups } });
  return data as WorkflowResponse;
}

export async function archiveWorkflow(id: string): Promise<void> {
  await api.DELETE("/workflows/{workflowID}", { params: { path: { workflowID: id } } });
}
