import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** A language model provider, as administrators see it; its key never leaves the server. */
export type Provider = components["schemas"]["ProviderResponse"];
export type ProviderRequest = components["schemas"]["ProviderRequest"];
export type ProviderTest = components["schemas"]["ProviderTestResponse"];

export const providersQueryKey = ["providers"] as const;

export const providersQuery = queryOptions({
  queryKey: providersQueryKey,
  queryFn: async (): Promise<Provider[]> => {
    const { data } = await api.GET("/providers");
    return (data?.providers ?? []) as Provider[];
  },
});

/** A model a workflow or one of its agents may name, as everyone signed in sees it. */
export type Model = components["schemas"]["ModelResponse"];

export const modelsQuery = queryOptions({
  queryKey: ["models"] as const,
  queryFn: async (): Promise<Model[]> => {
    const { data } = await api.GET("/models");
    return (data?.models ?? []) as Model[];
  },
});

export async function addProvider(body: ProviderRequest): Promise<Provider> {
  const { data } = await api.POST("/providers", { body });
  return data?.provider as Provider;
}

export async function changeProvider(id: string, body: ProviderRequest): Promise<Provider> {
  const { data } = await api.PUT("/providers/{providerID}", { params: { path: { providerID: id } }, body });
  return data?.provider as Provider;
}

export async function removeProvider(id: string): Promise<void> {
  await api.DELETE("/providers/{providerID}", { params: { path: { providerID: id } } });
}

/** Asks the provider which models it offers, with the key Rotor holds. */
export async function testProvider(id: string): Promise<ProviderTest> {
  const { data } = await api.POST("/providers/{providerID}/test", { params: { path: { providerID: id } } });
  return data as ProviderTest;
}
