import { createRoute } from "@tanstack/react-router";
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

// Two senders may post the same trace id, so the address names whose it is; a person's own needs no sender.
export const traceRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/traces/$traceID",
  validateSearch: (search: Record<string, unknown>): { sender?: string } => ({
    sender: typeof search.sender === "string" && search.sender !== "" ? search.sender : undefined,
  }),
  component: function TraceRoute() {
    const { traceID } = traceRoute.useParams();
    const { sender } = traceRoute.useSearch();
    return <TracePage key={`${sender ?? ""}/${traceID}`} traceID={traceID} sender={sender ?? ""} />;
  },
});
