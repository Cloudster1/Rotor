import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** A tool Rotor can act in; its client secret never leaves the server. */
export type Tool = components["schemas"]["ToolResponse"];
export type AddToolRequest = components["schemas"]["AddToolRequest"];
/** A workflow whose current version binds a tool by its name. */
export type ToolBinder = components["schemas"]["ToolBinder"];

export const toolsQueryKey = ["tools"] as const;

export const toolsQuery = queryOptions({
  queryKey: toolsQueryKey,
  queryFn: async (): Promise<Tool[]> => {
    const { data } = await api.GET("/tools");
    return (data?.tools ?? []) as Tool[];
  },
});

/** Adds a tool by its address, or registers a known one again; the server answers with the tool. */
export async function addTool(body: AddToolRequest): Promise<Tool> {
  const { data } = await api.POST("/tools", { body });
  return data?.tool as Tool;
}

/** Removes a tool, ending every connection to it. */
export async function removeTool(id: string): Promise<void> {
  await api.DELETE("/tools/{toolID}", { params: { path: { toolID: id } } });
}

/** Reads a tool's meta and metadata again, for a tool that was updated; the server answers with the tool. */
export async function checkTool(id: string): Promise<Tool> {
  const { data } = await api.POST("/tools/{toolID}/check", { params: { path: { toolID: id } } });
  return data?.tool as Tool;
}

/** The workflows binding the tool by its name now, which stop finding it once it is renamed (#267). */
export function toolWorkflowsQuery(id: string) {
  return queryOptions({
    queryKey: [...toolsQueryKey, id, "workflows"] as const,
    queryFn: async (): Promise<ToolBinder[]> => {
      const { data } = await api.GET("/tools/{toolID}/workflows", { params: { path: { toolID: id } } });
      return data?.workflows ?? [];
    },
  });
}

/** Renames a tool; the server answers with it and the workflows that still bind the old name. */
export async function renameTool(id: string, name: string): Promise<{ tool: Tool; workflows: ToolBinder[] }> {
  const { data } = await api.PUT("/tools/{toolID}/name", { params: { path: { toolID: id } }, body: { name } });
  return { tool: data?.tool as Tool, workflows: data?.workflows ?? [] };
}

/** The names several tools share from before names had to differ, case aside, with how many share each. */
export function sharedNames(tools: Pick<Tool, "name">[]): [string, number][] {
  const counts = new Map<string, [string, number]>();
  for (const t of tools) {
    const key = t.name.toLowerCase();
    const [first, n] = counts.get(key) ?? [t.name, 0];
    counts.set(key, [first, n + 1]);
  }
  return [...counts.values()].filter(([, n]) => n > 1);
}

/** Whether the tool lacks a capability Rotor needs, by its exact name@version. */
export function lacks(tool: Pick<Tool, "missing">, capability: string): boolean {
  return (tool.missing ?? []).some((m) => m.capability === capability);
}

/** A product's name as a sentence writes it: exciter is Exciter. */
export function toolName(tool: Pick<Tool, "name">): string {
  return tool.name.charAt(0).toUpperCase() + tool.name.slice(1);
}
