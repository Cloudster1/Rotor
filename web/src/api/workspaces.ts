import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** What workspaces may use (#136): toolchain images by name, the package mirror, a step's time and its work tree. */
export type WorkspaceSetting = components["schemas"]["WorkspaceSetting"];

export const workspacesQueryKey = ["settings", "workspaces"] as const;

export const workspacesQuery = queryOptions({
  queryKey: workspacesQueryKey,
  queryFn: async (): Promise<WorkspaceSetting> => {
    const { data } = await api.GET("/settings/workspaces");
    return data as WorkspaceSetting;
  },
});

export async function setWorkspaces(body: WorkspaceSetting): Promise<WorkspaceSetting> {
  const { data } = await api.PUT("/settings/workspaces", { body });
  return data as WorkspaceSetting;
}
