import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** One of the signed-in person's connections; the tokens behind it never leave the server. */
export type Connection = components["schemas"]["ConnectionResponse"];

export const connectionsQueryKey = ["connections"] as const;

export const connectionsQuery = queryOptions({
  queryKey: connectionsQueryKey,
  queryFn: async (): Promise<Connection[]> => {
    const { data } = await api.GET("/connections");
    return (data?.connections ?? []) as Connection[];
  },
});

/** Starts connecting to a tool; the answer is the tool's consent page, where the browser goes next. */
export async function startConnection(toolID: string): Promise<string> {
  const { data } = await api.POST("/tools/{toolID}/connection", { params: { path: { toolID } } });
  return data?.authorizeURL ?? "";
}

/** Ends the connection to a tool, revoking it there. */
export async function disconnect(toolID: string): Promise<void> {
  await api.DELETE("/tools/{toolID}/connection", { params: { path: { toolID } } });
}
