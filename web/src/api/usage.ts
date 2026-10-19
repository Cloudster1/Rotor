import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** What someone's runs used and the limits on them (docs/api-contract-m7-usage.md). */
export type Usage = components["schemas"]["UsageResponse"];
export type UsageDay = components["schemas"]["UsageDay"];
export type Limits = components["schemas"]["LimitsBody"];
export type PersonUsage = components["schemas"]["PersonUsage"];
export type ModelPrice = components["schemas"]["ModelPrice"];

export const usageQueryKey = ["usage"] as const;

/** The person's own usage, today, this month and of late. */
export const myUsageQuery = queryOptions({
  queryKey: [...usageQueryKey, "mine"],
  queryFn: async (): Promise<Usage> => {
    const { data } = await api.GET("/usage");
    return data as Usage;
  },
});

/** Everyone's usage this month, for administrators. */
export const peopleUsageQuery = queryOptions({
  queryKey: [...usageQueryKey, "people"],
  queryFn: async (): Promise<PersonUsage[]> => {
    const { data } = await api.GET("/usage/people");
    return (data as { people: PersonUsage[] }).people;
  },
});

export async function setLimits(body: Limits): Promise<Limits> {
  const { data } = await api.PUT("/settings/limits", { body });
  return data as Limits;
}

/** A provider's models' prices. */
export const pricesQuery = (providerID: string) =>
  queryOptions({
    queryKey: [...usageQueryKey, "prices", providerID],
    queryFn: async (): Promise<ModelPrice[]> => {
      const { data } = await api.GET("/providers/{providerID}/prices", { params: { path: { providerID } } });
      return (data as { prices: ModelPrice[] }).prices;
    },
  });

export async function setPrices(providerID: string, prices: ModelPrice[]): Promise<ModelPrice[]> {
  const { data } = await api.PUT("/providers/{providerID}/prices", { params: { path: { providerID } }, body: { prices } });
  return (data as { prices: ModelPrice[] }).prices;
}
