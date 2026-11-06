import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { type WorkflowResponse, workflowQuery } from "@/api/workflows";

/** The workflow the address names, as every section reads it; the layout waits for it, so a section always has it. */
export function useSettingsWorkflow(): { workflowID: string; existing: WorkflowResponse | undefined } {
  const { workflowID } = useParams({ from: "/shell/workflows/$workflowID/settings" });
  const workflow = useQuery(workflowQuery(workflowID));
  return { workflowID, existing: workflow.data };
}

/** A note that says why a section is read-only, or what its rules are. */
export function SectionNote({ children, ...data }: { children: string } & Record<`data-${string}`, string>) {
  return (
    <p className="max-w-3xl rounded-control border border-border bg-surface-raised px-3 py-2 text-sm text-ink lg:col-span-full" {...data}>
      {children}
    </p>
  );
}
