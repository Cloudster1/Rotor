import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** How many days a finished run's data is kept before Rotor deletes it (#41), as administrators see it. */
export type Retention = components["schemas"]["RetentionResponse"];

export const retentionQueryKey = ["settings", "retention"] as const;

export const retentionQuery = queryOptions({
  queryKey: retentionQueryKey,
  queryFn: async (): Promise<Retention> => {
    const { data } = await api.GET("/settings/retention");
    return data as Retention;
  },
});

export async function setRetention(runDataDays: number): Promise<Retention> {
  const { data } = await api.PUT("/settings/retention", { body: { runDataDays } });
  return data as Retention;
}
