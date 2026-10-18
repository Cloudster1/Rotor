import { createRoute, useParams } from "@tanstack/react-router";
import { RunPage } from "@/features/runs/RunPage";
import { shellRoute } from "./shell";

function RunRoute() {
  const { runID } = useParams({ from: "/shell/runs/$runID" });
  return <RunPage runID={runID} />;
}

export const runRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/runs/$runID",
  component: RunRoute,
});
