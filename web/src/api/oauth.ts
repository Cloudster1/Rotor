import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** An assistant's request to connect, waiting for its person (#47). */
export type AuthorizationRequest = Wire["OAuthAuthorizationRequest"];
/** An assistant the caller connected. */
export type Grant = Wire["OAuthGrant"];
/** An assistant in everybody's list, with whose it is. */
export type OwnedGrant = Wire["OAuthOwnedGrant"];
/** Whether assistants may connect at all. */
export type AssistantSetting = Wire["AssistantSetting"];

export const grantsQueryKey = ["oauth", "grants"] as const;
export const everyonesGrantsQueryKey = [...grantsQueryKey, "everyone"] as const;
export const assistantSettingQueryKey = ["settings", "assistants"] as const;

export const authorizationRequestQuery = (id: string) =>
  queryOptions({
    queryKey: ["oauth", "request", id],
    retry: false,
    queryFn: async (): Promise<AuthorizationRequest> => {
      const { data } = await api.GET("/oauth/requests/{requestID}", { params: { path: { requestID: id } } });
      return (data as { request: AuthorizationRequest }).request;
    },
  });

/** Allows or denies a request, and answers where the browser goes back to. */
export async function decideAuthorization(id: string, allow: boolean, readOnly: boolean): Promise<string> {
  const { data } = await api.POST("/oauth/requests/{requestID}", { params: { path: { requestID: id } }, body: { allow, readOnly } });
  return (data as { redirectTo: string }).redirectTo;
}

export const grantsQuery = queryOptions({
  queryKey: [...grantsQueryKey, "mine"],
  queryFn: async (): Promise<Grant[]> => {
    const { data } = await api.GET("/oauth/grants");
    return (data as { grants: Grant[] }).grants;
  },
});

export const everyonesGrantsQuery = queryOptions({
  queryKey: everyonesGrantsQueryKey,
  queryFn: async (): Promise<OwnedGrant[]> => {
    const { data } = await api.GET("/oauth/grants/everyone");
    return (data as { grants: OwnedGrant[] }).grants;
  },
});

/** Ends one of the caller's grants, or, with anybodys, anybody's as an administrator. */
export async function endGrant(id: string, anybodys = false): Promise<void> {
  if (anybodys) await api.DELETE("/oauth/grants/everyone/{grantID}", { params: { path: { grantID: id } } });
  else await api.DELETE("/oauth/grants/{grantID}", { params: { path: { grantID: id } } });
}

export const assistantSettingQuery = queryOptions({
  queryKey: assistantSettingQueryKey,
  queryFn: async (): Promise<AssistantSetting> => {
    const { data } = await api.GET("/settings/assistants");
    return data as AssistantSetting;
  },
});

export async function setAssistants(connectionsOn: boolean): Promise<AssistantSetting> {
  const { data } = await api.PUT("/settings/assistants", { body: { connectionsOn } });
  return data as AssistantSetting;
}
