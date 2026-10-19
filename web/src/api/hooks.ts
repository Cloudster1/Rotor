import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** A tool's webhook as an administrator sees it: where it delivers, and whether Rotor holds its secret. */
export type Hook = components["schemas"]["HookResponse"];

export const hookQueryKey = (toolID: string) => ["tools", toolID, "hook"] as const;

export const hookQuery = (toolID: string) =>
  queryOptions({
    queryKey: hookQueryKey(toolID),
    queryFn: async (): Promise<Hook> => {
      const { data } = await api.GET("/tools/{toolID}/hook", { params: { path: { toolID } } });
      return data as Hook;
    },
  });

/** Keeps the secret the tool made for Rotor's webhook; the server never answers it back. */
export async function setHookSecret(toolID: string, secret: string): Promise<Hook> {
  const { data } = await api.PUT("/tools/{toolID}/hook", { params: { path: { toolID } }, body: { secret } });
  return data as Hook;
}

/** Forgets the secret, so the tool's deliveries are refused. */
export async function removeHookSecret(toolID: string): Promise<Hook> {
  const { data } = await api.DELETE("/tools/{toolID}/hook", { params: { path: { toolID } } });
  return data as Hook;
}
