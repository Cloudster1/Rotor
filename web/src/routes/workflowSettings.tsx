import { createRoute, lazyRouteComponent, redirect } from "@tanstack/react-router";
import { shellRoute } from "./shell";

const settingsModule = () => import("@/features/workflows/settings/WorkflowSettings");

/** A workflow's own settings (#205): a side nav of its sections, each a child route, for everybody who sees it. */
export const workflowSettingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/workflows/$workflowID/settings",
  component: lazyRouteComponent(settingsModule, "WorkflowSettingsLayout"),
});

const indexRoute = createRoute({
  getParentRoute: () => workflowSettingsRoute,
  path: "/",
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/workflows/$workflowID/settings/general", params, replace: true });
  },
});

function section<TPath extends string>(path: TPath, name: "GeneralPage" | "VersionsPage" | "SharingPage" | "TriggersPage" | "TracingPage") {
  return createRoute({ getParentRoute: () => workflowSettingsRoute, path, component: lazyRouteComponent(settingsModule, name) });
}

export const workflowSettingsTree = workflowSettingsRoute.addChildren([
  indexRoute,
  section("general", "GeneralPage"),
  section("versions", "VersionsPage"),
  section("sharing", "SharingPage"),
  section("triggers", "TriggersPage"),
  section("tracing", "TracingPage"),
]);
