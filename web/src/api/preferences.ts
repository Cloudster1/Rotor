import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** What a person chose for themselves (#206): the time zone dates are written in, null following the browser's. */
export type Preferences = components["schemas"]["PreferencesBody"];

export const preferencesQueryKey = ["auth", "me", "preferences"] as const;

export const preferencesQuery = queryOptions({
  queryKey: preferencesQueryKey,
  queryFn: async (): Promise<Preferences> => {
    const { data } = await api.GET("/auth/me/preferences");
    return data as Preferences;
  },
});

export async function setPreferences(body: Preferences): Promise<Preferences> {
  const { data } = await api.PUT("/auth/me/preferences", { body });
  return data as Preferences;
}
