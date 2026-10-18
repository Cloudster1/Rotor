import { createRoute, redirect } from "@tanstack/react-router";
import { HOME_PATH } from "@/config";
import { SettingsPage } from "@/features/settings/SettingsPage";
import { EmptyState, PageHeader } from "@/features/shell/EmptyState";
import { Icon, type IconName } from "@/features/shell/icons";
import { s } from "@/strings";
import { shellRoute } from "./shell";

/** The index has nothing of its own; it is the first place of the sidebar. */
export const indexRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: HOME_PATH });
  },
});

// A place whose feature has not landed yet: its heading and what will be here.
function emptyPlace<P extends string>(path: P, title: string, icon: IconName, empty: { emptyTitle: string; emptyBody: string }) {
  const Glyph = Icon[icon];
  return createRoute({
    getParentRoute: () => shellRoute,
    path,
    component: () => (
      <div>
        <PageHeader title={title} />
        <EmptyState icon={<Glyph />} title={empty.emptyTitle} description={empty.emptyBody} />
      </div>
    ),
  });
}

export const workflowsRoute = emptyPlace("/workflows", s.places.workflows, "Workflow", s.workflows);
export const runsRoute = emptyPlace("/runs", s.places.runs, "Run", s.runs);
export const approvalsRoute = emptyPlace("/approvals", s.places.approvals, "Approval", s.approvals);
export const connectionsRoute = emptyPlace("/connections", s.places.connections, "Connection", s.connections);

export const settingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings",
  component: SettingsPage,
});
