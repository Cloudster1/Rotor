import { useParams } from "@tanstack/react-router";
import { RunPage } from "./RunPage";

/** A run by the id in the address; loaded on demand with the graph. */
export function RunRoute() {
  const { runID } = useParams({ from: "/shell/runs/$runID" });
  return <RunPage runID={runID} />;
}
