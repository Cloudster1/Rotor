import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** A tool Rotor can act in; its client secret never leaves the server. */
export type Tool = components["schemas"]["ToolResponse"];
export type AddToolRequest = components["schemas"]["AddToolRequest"];

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

/** A product's name as a sentence writes it: exciter is Exciter. */
export function toolName(tool: Pick<Tool, "name">): string {
  return tool.name.charAt(0).toUpperCase() + tool.name.slice(1);
}
