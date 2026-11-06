import { useParams, useSearch } from "@tanstack/react-router";
import { RunPage } from "./RunPage";

/** A run by the id in the address, and a span of its trace to choose; loaded on demand with the graph. */
export function RunRoute() {
  const { runID } = useParams({ from: "/shell/runs/$runID" });
  const { span } = useSearch({ from: "/shell/runs/$runID" });
  return <RunPage runID={runID} focus={span} />;
}
