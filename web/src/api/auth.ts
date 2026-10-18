import { queryOptions } from "@tanstack/react-query";
import { API_BASE } from "@/config";
import { HANDLES_UNAUTHORIZED } from "@/lib/session";
import { api } from "./client";
import type { components } from "./schema";

/** The signed-in person, with the role and groups the realm gave them. */
export type Me = components["schemas"]["MeResponse"];

export const meQueryKey = ["auth", "me"] as const;

/** Who is signed in; a 401 here is the answer "nobody", which the guard turns into the sign-in page. */
export const meQuery = queryOptions({
  queryKey: meQueryKey,
  queryFn: async (): Promise<Me> => {
    const { data } = await api.GET("/auth/me");
    return data as Me;
  },
  retry: false,
  meta: HANDLES_UNAUTHORIZED,
});

/** Ends this browser's session. */
export async function signOut(): Promise<void> {
  await api.POST("/auth/logout");
}

/** Where the browser goes to sign in through the suite's identity provider, landing on next afterwards. */
export function signInURL(next?: string): string {
  return `${API_BASE}/auth/oidc/start${next ? `?next=${encodeURIComponent(next)}` : ""}`;
}
