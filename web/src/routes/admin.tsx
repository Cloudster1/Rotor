import { createRoute, redirect } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { AuditLog } from "@/features/audit/AuditLog";
import { AllAssistantsSection } from "@/features/oauth/AllAssistantsSection";
import { ProvidersSection } from "@/features/providers/ProvidersSection";
import { FIRST_ADMIN_PATH } from "@/features/settings/groups";
import { LangSmithSection } from "@/features/settings/LangSmithSection";
import { RetentionSection } from "@/features/settings/RetentionSection";
import { AdminLayout } from "@/features/settings/SettingsLayout";
import { SettingsPage } from "@/features/settings/ui";
import { UnattendedWritesSection } from "@/features/settings/UnattendedWritesSection";
import { WorkspacesSection } from "@/features/settings/WorkspacesSection";
import { ToolsSection } from "@/features/tools/ToolsSection";
import { EveryonesUsagePage } from "@/features/usage/UsagePage";
import { s } from "@/strings";
import { shellRoute } from "./shell";

/** The installation's pages (#199), for administrators; the layout refuses anybody else. */
export const adminRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/admin",
  component: AdminLayout,
});

const adminIndexRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: FIRST_ADMIN_PATH, replace: true });
  },
});

/** A page of one section, under its own heading. */
function page<TPath extends string>(path: TPath, title: string, section: () => ReactNode) {
  return createRoute({
    getParentRoute: () => adminRoute,
    path,
    component: () => <SettingsPage title={title}>{section()}</SettingsPage>,
  });
}

export const adminTree = adminRoute.addChildren([
  adminIndexRoute,
  page("workspaces", s.admin.pages.workspaces, () => <WorkspacesSection />),
  page("unattended-writes", s.admin.pages.unattendedWrites, () => <UnattendedWritesSection />),
  page("providers", s.admin.pages.providers, () => <ProvidersSection />),
  page("langsmith", s.admin.pages.langsmith, () => <LangSmithSection />),
  page("tools", s.admin.pages.tools, () => <ToolsSection />),
  page("assistants", s.admin.pages.assistants, () => <AllAssistantsSection />),
  createRoute({ getParentRoute: () => adminRoute, path: "audit", component: AuditLog }),
  createRoute({ getParentRoute: () => adminRoute, path: "usage", component: EveryonesUsagePage }),
  page("retention", s.admin.pages.retention, () => <RetentionSection />),
]);
