import { useParams } from "@tanstack/react-router";
import { WorkflowEditor, WorkflowPage } from "./WorkflowPage";

/** A new workflow, from the template; loaded on demand with the editor. */
export function NewWorkflowRoute() {
  return <WorkflowEditor />;
}

/** An existing workflow by the id in the address; loaded on demand with the editor. */
export function ExistingWorkflowRoute() {
  const { workflowID } = useParams({ from: "/shell/workflows/$workflowID" });
  return <WorkflowPage workflowID={workflowID} />;
}
