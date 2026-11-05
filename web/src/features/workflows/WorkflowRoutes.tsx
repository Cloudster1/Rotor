import { useParams, useSearch } from "@tanstack/react-router";
import { WorkflowEditor, WorkflowPage } from "./WorkflowPage";

/** A new workflow, from the blank template or the shipped one the address names; loaded on demand with the editor. */
export function NewWorkflowRoute() {
  const { start } = useSearch({ from: "/shell/workflows/new" });
  return <WorkflowEditor key={start ?? ""} start={start} />;
}

/** An existing workflow by the id in the address; loaded on demand with the editor. */
export function ExistingWorkflowRoute() {
  const { workflowID } = useParams({ from: "/shell/workflows/$workflowID" });
  const { version } = useSearch({ from: "/shell/workflows/$workflowID" });
  return <WorkflowPage workflowID={workflowID} openVersion={version} />;
}
