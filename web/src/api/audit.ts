import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { API_BASE } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

/** One entry of the audit log, and a window of them with the next window's cursor (#36). */
export type AuditEntry = components["schemas"]["AuditEntry"];
export type AuditWindow = components["schemas"]["AuditWindow"];
export type AuditFacets = components["schemas"]["AuditFacets"];

/** What narrows the log; days are YYYY-MM-DD, read by the server as whole days in UTC. */
export interface AuditFilter {
  action?: string;
  actor?: string;
  targetType?: string;
  target?: string;
  from?: string;
  to?: string;
}

export const auditQueryKey = ["audit"] as const;

/** One window of the log, after the cursor the window before answered. */
export const auditQuery = (filter: AuditFilter, cursor?: string) =>
  queryOptions({
    queryKey: [...auditQueryKey, "window", filter, cursor ?? ""],
    queryFn: async (): Promise<AuditWindow> => {
      const { data } = await api.GET("/audit", { params: { query: { ...filter, cursor } } });
      return data as AuditWindow;
    },
    placeholderData: keepPreviousData,
  });

export const auditFacetsQuery = queryOptions({
  queryKey: [...auditQueryKey, "facets"],
  queryFn: async (): Promise<AuditFacets> => {
    const { data } = await api.GET("/audit/facets");
    return data as AuditFacets;
  },
});

/** Where the browser downloads the filtered log; a plain link, so the file saves as the server names it. */
export function auditExportHref(filter: AuditFilter, format: "csv" | "json"): string {
  const query = new URLSearchParams({ format });
  for (const [key, value] of Object.entries(filter)) {
    if (value) query.set(key, value);
  }
  return `${API_BASE}/audit/export?${query.toString()}`;
}
