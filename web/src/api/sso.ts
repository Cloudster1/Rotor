import { queryOptions } from "@tanstack/react-query";
import { accessQueryKey } from "./access";
import { api } from "./client";
import type { components } from "./schema";

/** Single sign-on as Admin's Access page keeps it (#204); the client secret is never in it, only whether one is held. */
export type SingleSignOn = components["schemas"]["OidcProviderResponse"];
export type Provider = components["schemas"]["OidcProviderView"];
export type ProviderInput = components["schemas"]["SaveOIDCProviderRequest"];

export const singleSignOnQueryKey = [...accessQueryKey, "sso"] as const;

export const singleSignOnQuery = queryOptions({
  queryKey: singleSignOnQueryKey,
  queryFn: async (): Promise<SingleSignOn> => {
    const { data } = await api.GET("/oidc-provider");
    return data as SingleSignOn;
  },
});

/** Sets single sign-on up or changes it; a blank secret keeps the one held, and it applies from the next sign-in. */
export async function saveSingleSignOn(input: ProviderInput): Promise<SingleSignOn> {
  const { data } = await api.PUT("/oidc-provider", { body: input });
  return data as SingleSignOn;
}
