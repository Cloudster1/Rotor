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
  component: lazyRouteComponent(() => import("@/features/workflows/WorkflowRoutes"), "NewWorkflowRoute"),
});

export const workflowRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/workflows/$workflowID",
  component: lazyRouteComponent(() => import("@/features/workflows/WorkflowRoutes"), "ExistingWorkflowRoute"),
});
