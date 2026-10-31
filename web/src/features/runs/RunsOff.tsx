import { useQuery } from "@tanstack/react-query";
import { metaQuery } from "@/api/meta";
import { s } from "@/strings";

/** Whether this installation runs workflows; true until /meta says otherwise, so a slow answer never blocks a start. */
export function useRunsOn(): boolean {
  return useQuery(metaQuery).data?.runs !== false;
}

/** Says before anyone starts a run that this installation runs none, and what to do about it. */
export function RunsOff() {
  return (
    <p role="status" className="max-w-3xl rounded-control border border-warning bg-warning-subtle px-3 py-2 text-sm text-ink" data-runs-off>
      {s.runs.off}
    </p>
  );
}
