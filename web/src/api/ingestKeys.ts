import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** A key a service sends spans with (#258); the key itself comes back once, when it is made or rotated. */
export type IngestKey = components["schemas"]["IngestKey"];
export type IngestKeyRequest = components["schemas"]["IngestKeyRequest"];
export type IngestKeySecret = components["schemas"]["IngestKeySecret"];

export const ingestKeysQueryKey = ["settings", "ingest-keys"] as const;

export const ingestKeysQuery = queryOptions({
  queryKey: ingestKeysQueryKey,
  queryFn: async (): Promise<IngestKey[]> => {
    const { data } = await api.GET("/settings/ingest-keys");
    return (data?.keys ?? []) as IngestKey[];
  },
});

export async function createIngestKey(body: IngestKeyRequest): Promise<IngestKeySecret> {
  const { data } = await api.POST("/settings/ingest-keys", { body });
  return data as IngestKeySecret;
}

export async function rotateIngestKey(keyID: string): Promise<IngestKeySecret> {
  const { data } = await api.POST("/settings/ingest-keys/{keyID}/rotate", { params: { path: { keyID } } });
  return data as IngestKeySecret;
}

export async function revokeIngestKey(keyID: string): Promise<void> {
  await api.DELETE("/settings/ingest-keys/{keyID}", { params: { path: { keyID } } });
}
