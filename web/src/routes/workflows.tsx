import { createRoute, lazyRouteComponent } from "@tanstack/react-router";
import { WorkflowsPage } from "@/features/workflows/WorkflowsPage";
import { shellRoute } from "./shell";

export const workflowsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/workflows",
  component: WorkflowsPage,
});

// The editor brings CodeMirror and the schema's hover formatting, so only the pages that edit load it.
export const newWorkflowRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/workflows/new",
  // A shipped workflow's name opens the editor with it; the editor falls back to blank for a name it does not know.
  validateSearch: (search: Record<string, unknown>): { start?: string } =>
    typeof search.start === "string" && search.start !== "" ? { start: search.start } : {},
  component: lazyRouteComponent(() => import("@/features/workflows/WorkflowRoutes"), "NewWorkflowRoute"),
});

// An older version opens in the editor from the Versions settings (#205), named in the address so a reload keeps it.
export const workflowRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/workflows/$workflowID",
  validateSearch: (search: Record<string, unknown>): { version?: number } =>
    typeof search.version === "number" && Number.isInteger(search.version) && search.version > 0 ? { version: search.version } : {},
  component: lazyRouteComponent(() => import("@/features/workflows/WorkflowRoutes"), "ExistingWorkflowRoute"),
});
