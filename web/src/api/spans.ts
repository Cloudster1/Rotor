import { infiniteQueryOptions } from "@tanstack/react-query";
import { SPAN_SEARCH_PAGE_SIZE, SPAN_SEARCH_SLACK_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

/** A span a search found (#261), with the run or the sender's own trace it belongs to. */
export type SpanHit = components["schemas"]["SpanHit"];
export type SpanSearchResponse = components["schemas"]["SpanSearchResponse"];

const DAY_MS = 86_400_000;

/** What Search spans asks, as its address keeps it; days is the window back from now. */
export interface SpanSearch {
  name?: string;
  kind?: SpanHit["kind"];
  failed?: true;
  model?: string;
  service?: string;
  workflow?: string;
  attrKey?: string;
  attrValue?: string;
  days?: number;
}

/** The query the API reads for a search; the window starts as the page is asked for, so a later page reads the same. */
export function spanSearchQueryOf(search: SpanSearch, days: number, now: number): Record<string, string> {
  const query: Record<string, string> = { since: new Date(now - days * DAY_MS + SPAN_SEARCH_SLACK_MS).toISOString(), limit: String(SPAN_SEARCH_PAGE_SIZE) };
  for (const key of ["name", "kind", "model", "service", "workflow", "attrKey", "attrValue"] as const) {
    const value = search[key];
    if (value) query[key] = value;
  }
  if (search.failed) query.status = "error";
  return query;
}

/** The pages of a search, each going on from the cursor the one before answered. */
export const spanSearchQuery = (search: SpanSearch, days: number) =>
  infiniteQueryOptions({
    queryKey: ["spans", search, days] as const,
    initialPageParam: { cursor: "", at: 0 },
    queryFn: async ({ pageParam }): Promise<SpanSearchResponse & { at: number }> => {
      // The first page fixes when the window began, so every later page reads the same window.
      const at = pageParam.at || Date.now();
      const query = spanSearchQueryOf(search, days, at);
      if (pageParam.cursor) query.cursor = pageParam.cursor;
      const { data } = await api.GET("/spans", { params: { query } });
      return { ...(data as SpanSearchResponse), at };
    },
    getNextPageParam: (last) => (last.next ? { cursor: last.next, at: last.at } : undefined),
  });
