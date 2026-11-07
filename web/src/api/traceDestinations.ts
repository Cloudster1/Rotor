import { queryOptions } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** A tool that receives every run's trace over OTLP (#246); header values never come back, only their names. */
export type TraceDestination = components["schemas"]["TraceDestination"];
export type TraceDestinationRequest = components["schemas"]["TraceDestinationRequest"];
export type TraceDestinationTest = components["schemas"]["TraceDestinationTest"];

export const traceDestinationsQueryKey = ["settings", "trace-destinations"] as const;

export const traceDestinationsQuery = queryOptions({
  queryKey: traceDestinationsQueryKey,
  queryFn: async (): Promise<TraceDestination[]> => {
    const { data } = await api.GET("/settings/trace-destinations");
    return (data?.destinations ?? []) as TraceDestination[];
  },
});

export async function addTraceDestination(body: TraceDestinationRequest): Promise<TraceDestination> {
  const { data } = await api.POST("/settings/trace-destinations", { body });
  return (data as { destination: TraceDestination }).destination;
}

/** A body without headers keeps the ones held; an empty map removes them. */
export async function changeTraceDestination(destinationID: string, body: TraceDestinationRequest): Promise<TraceDestination> {
  const { data } = await api.PUT("/settings/trace-destinations/{destinationID}", { params: { path: { destinationID } }, body });
  return (data as { destination: TraceDestination }).destination;
}

export async function removeTraceDestination(destinationID: string): Promise<void> {
  await api.DELETE("/settings/trace-destinations/{destinationID}", { params: { path: { destinationID } } });
}

export async function testTraceDestination(destinationID: string): Promise<TraceDestinationTest> {
  const { data } = await api.POST("/settings/trace-destinations/{destinationID}/test", { params: { path: { destinationID } } });
  return data as TraceDestinationTest;
}
