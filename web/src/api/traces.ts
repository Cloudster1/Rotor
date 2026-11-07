import { keepPreviousData, queryOptions, type UseQueryOptions } from "@tanstack/react-query";
import { api } from "./client";
import { runsQueryKey } from "./runs";
import type { components } from "./schema";

/** A run's trace as a tree of spans (#244), with previews of each input and output. */
export type Trace = components["schemas"]["TraceTree"];
/** One span of the tree, with its children. */
export type TraceSpan = components["schemas"]["TraceSpan"];
/** One span whole, with its text, as the span route answers it. */
export type TraceSpanDetail = components["schemas"]["TraceSpanDetail"];
export type SpanValue = components["schemas"]["SpanValue"];
export type SpanEvent = components["schemas"]["SpanEvent"];
export type SpanLink = components["schemas"]["SpanLink"];
export type SpanKind = TraceSpan["kind"];
/** A trace a sender posted of no run (#243), as the list of them answers it. */
export type TraceSummary = components["schemas"]["TraceSummary"];
export type TraceList = components["schemas"]["TraceList"];
/** How a trace view asks for one span whole, a run's trace and an owned one alike; the options are invariant in the key, which both make string[]. */
export type SpanQuery = (spanID: string) => UseQueryOptions<TraceSpanDetail, Error, TraceSpanDetail, string[]>;
/** How long traces are kept and whether their text is (#242), which anyone signed in reads. */
export type TraceSetting = components["schemas"]["TraceSettingResponse"];

// Under the run's own key, so whatever asks for the run again asks for its trace too.
const traceKey = (runID: string) => [...runsQueryKey, runID, "trace"] as const;

/** A run's trace; poll says, each time it is asked, how soon to ask again while spans may still arrive, false once not. */
export const runTraceQuery = (runID: string, poll: () => number | false) =>
  queryOptions({
    queryKey: traceKey(runID),
    queryFn: async (): Promise<Trace> => {
      const { data } = await api.GET("/runs/{runID}/trace", { params: { path: { runID } } });
      return (data as { trace: Trace }).trace;
    },
    refetchInterval: poll,
  });

/** One span of a run's trace whole; a span never changes once kept, so its answer stays fresh. */
export const runTraceSpanQuery = (runID: string, spanID: string) =>
  queryOptions({
    queryKey: [...traceKey(runID), spanID],
    queryFn: async (): Promise<TraceSpanDetail> => {
      const { data } = await api.GET("/runs/{runID}/trace/spans/{spanID}", { params: { path: { runID, spanID } } });
      return (data as { span: TraceSpanDetail }).span;
    },
    staleTime: Number.POSITIVE_INFINITY,
  });

export const traceSettingQuery = queryOptions({
  queryKey: ["settings", "traces"] as const,
  queryFn: async (): Promise<TraceSetting> => {
    const { data } = await api.GET("/settings/traces");
    return data as TraceSetting;
  },
});

export const traceSettingQueryKey = traceSettingQuery.queryKey;

export async function setTraceSetting(body: components["schemas"]["TraceSettingRequest"]): Promise<TraceSetting> {
  const { data } = await api.PUT("/settings/traces", { body });
  return data as TraceSetting;
}

const ownTracesKey = ["traces"] as const;

/** One page of the owned traces the person reads, newest first; sender narrows an administrator's to one sender. */
export const ownTracesQuery = (filter: { sender?: string; offset: number; limit: number }) =>
  queryOptions({
    queryKey: [...ownTracesKey, filter] as const,
    queryFn: async (): Promise<TraceList> => {
      const { data } = await api.GET("/traces", { params: { query: filter } });
      return data as TraceList;
    },
    // The page in hand stays while the next is read, so the pager does not jump.
    placeholderData: keepPreviousData,
  });

// An administrator names whose trace it is, as two senders may post the same trace id.
const ownTraceKey = (traceID: string, sender: string) => [...ownTracesKey, sender, traceID] as const;

/** An owned trace as a tree; sender is its sender's user id, which the person's own needs not but may carry. */
export const ownTraceQuery = (traceID: string, sender: string) =>
  queryOptions({
    queryKey: ownTraceKey(traceID, sender),
    queryFn: async (): Promise<Trace> => {
      const { data } = await api.GET("/traces/{traceID}", { params: { path: { traceID }, query: sender ? { sender } : {} } });
      return (data as { trace: Trace }).trace;
    },
  });

/** One span of an owned trace whole; like a run's, it never changes once kept. */
export const ownTraceSpanQuery = (traceID: string, sender: string, spanID: string) =>
  queryOptions({
    queryKey: [...ownTraceKey(traceID, sender), spanID],
    queryFn: async (): Promise<TraceSpanDetail> => {
      const { data } = await api.GET("/traces/{traceID}/spans/{spanID}", { params: { path: { traceID, spanID }, query: sender ? { sender } : {} } });
      return (data as { span: TraceSpanDetail }).span;
    },
    staleTime: Number.POSITIVE_INFINITY,
  });
