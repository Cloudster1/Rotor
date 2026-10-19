import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** Whether runs' spans also go to LangSmith, as administrators see it; the key never comes back. */
export type LangSmith = components["schemas"]["LangSmithResponse"];
export type LangSmithRequest = components["schemas"]["LangSmithRequest"];

export const langSmithQueryKey = ["settings", "langsmith"] as const;

export const langSmithQuery = queryOptions({
  queryKey: langSmithQueryKey,
  queryFn: async (): Promise<LangSmith> => {
    const { data } = await api.GET("/settings/langsmith");
    return data as LangSmith;
  },
});

export async function setLangSmith(body: LangSmithRequest): Promise<LangSmith> {
  const { data } = await api.PUT("/settings/langsmith", { body });
  return data as LangSmith;
}

export async function removeLangSmithKey(): Promise<LangSmith> {
  const { data } = await api.DELETE("/settings/langsmith/key");
  return data as LangSmith;
}
