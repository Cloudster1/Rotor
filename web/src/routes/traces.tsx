import { createRoute } from "@tanstack/react-router";
import { SpanSearchPage, spanSearch } from "@/features/traces/SpanSearchPage";
import { TracePage } from "@/features/traces/TracePage";
import { TracesPage, tracesSearch } from "@/features/traces/TracesPage";
import { shellRoute } from "./shell";

export const tracesRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/traces",
  validateSearch: tracesSearch,
  component: function TracesRoute() {
    return <TracesPage search={tracesRoute.useSearch()} />;
  },
});

// Search spans (#261) sits under Traces, so the sidebar shows where it is; no trace id is the word search.
export const spanSearchRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/traces/search",
  validateSearch: spanSearch,
  component: function SpanSearchRoute() {
    return <SpanSearchPage search={spanSearchRoute.useSearch()} />;
  },
});

/** A span the address names, to choose in a trace's tree, as a search's hit opens it. */
export function spanParam(search: Record<string, unknown>): string | undefined {
  return typeof search.span === "string" && search.span !== "" ? search.span : undefined;
}

// Two senders may post the same trace id, so the address names whose it is; a person's own needs no sender.
export const traceRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/traces/$traceID",
  validateSearch: (search: Record<string, unknown>): { sender?: string; span?: string } => ({
    sender: typeof search.sender === "string" && search.sender !== "" ? search.sender : undefined,
    span: spanParam(search),
  }),
  component: function TraceRoute() {
    const { traceID } = traceRoute.useParams();
    const { sender, span } = traceRoute.useSearch();
    return <TracePage key={`${sender ?? ""}/${traceID}`} traceID={traceID} sender={sender ?? ""} focus={span} />;
  },
});
