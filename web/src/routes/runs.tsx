import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { RunsPage, runsSearch } from "@/features/runs/RunsPage";
import { shellRoute } from "./shell";

export const runsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/runs",
  validateSearch: runsSearch,
  component: function RunsRoute() {
    return <RunsPage search={runsRoute.useSearch()} />;
  },
});

// The live page brings React Flow, so only a run's own page loads it.
export const runRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/runs/$runID",
  component: lazyRouteComponent(() => import("@/features/runs/RunRoute"), "RunRoute"),
});
