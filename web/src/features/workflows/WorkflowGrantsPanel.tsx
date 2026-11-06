import { useQuery, useQueryClient } from "@tanstack/react-query";
import { accessQueryKey, grantOnWorkflow, revokeOnWorkflow, workflowGrantsQuery } from "@/api/access";
import { type WorkflowSummary, workflowsQueryKey } from "@/api/workflows";
import { WORKFLOW_GRANT_ROLES } from "@/config";
import { GrantsPanel } from "@/features/access/GrantsPanel";
import { s } from "@/strings";

/**
 * Who holds a role on this workflow, which is what sharing it means since #203. Anybody who sees the workflow
 * reads the list; its workflow administrators add people or groups as Runner, Editor or Workflow administrator.
 */
export function WorkflowGrantsPanel({ workflow }: { workflow: WorkflowSummary }) {
  const client = useQueryClient();
  const grants = useQuery(workflowGrantsQuery(workflow.id));
  // A grant changes what the workflow's list says about it and what people may do with it.
  const refresh = () => Promise.all([client.invalidateQueries({ queryKey: accessQueryKey }), client.invalidateQueries({ queryKey: workflowsQueryKey })]);
  return (
    <GrantsPanel
      grants={grants}
      roles={WORKFLOW_GRANT_ROLES}
      administers={workflow.canAdminister && !workflow.archived}
      words={s.workflowGrants}
      grant={(body) => grantOnWorkflow(workflow.id, body)}
      revoke={(grantID) => revokeOnWorkflow(workflow.id, grantID)}
      refresh={refresh}
      data-workflow-grants=""
    />
  );
}
