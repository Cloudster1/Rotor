import { queryOptions } from "@tanstack/react-query";
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
export type SpanKind = TraceSpan["kind"];
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
