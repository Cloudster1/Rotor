import { createRoute, redirect, useNavigate } from "@tanstack/react-router";
import { type ReactNode, useCallback } from "react";
import { ACCESS_TABS, AccessPage, type AccessTab } from "@/features/access/AccessPage";
import { AuditLog } from "@/features/audit/AuditLog";
import { AllAssistantsSection } from "@/features/oauth/AllAssistantsSection";
import { ProvidersSection } from "@/features/providers/ProvidersSection";
import { FIRST_ADMIN_PATH } from "@/features/settings/groups";
import { RetentionSection } from "@/features/settings/RetentionSection";
import { AdminLayout } from "@/features/settings/SettingsLayout";
import { SettingsPage } from "@/features/settings/ui";
import { TraceDestinationsSection } from "@/features/settings/TraceDestinationsSection";
import { UnattendedWritesSection } from "@/features/settings/UnattendedWritesSection";
import { WorkspacesSection } from "@/features/settings/WorkspacesSection";
import { ToolsSection } from "@/features/tools/ToolsSection";
import type { UsersShown } from "@/features/users/UserTable";
import { UsersPage } from "@/features/users/UsersPage";
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

// The tab is in the address, so a link opens the same view; Roles is the address without one.
const accessRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "access",
  validateSearch: (search: Record<string, unknown>): { tab?: AccessTab } =>
    ACCESS_TABS.some((tab) => tab === search.tab && tab !== "roles") ? { tab: search.tab as AccessTab } : {},
  component: function Access() {
    const { tab = "roles" } = accessRoute.useSearch();
    const navigate = useNavigate();
    return <AccessPage tab={tab} onTab={(next) => navigate({ to: "/admin/access", search: { tab: next === "roles" ? undefined : next } })} />;
  },
});

// The search and the page are in the address, so a link reopens the same list.
const usersRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "users",
  validateSearch: (search: Record<string, unknown>): UsersShown => ({
    // A search of digits alone comes back from the address as a number.
    ...(typeof search.q === "number" ? { q: String(search.q) } : typeof search.q === "string" && search.q !== "" ? { q: search.q } : {}),
    ...(typeof search.offset === "number" && Number.isInteger(search.offset) && search.offset > 0 ? { offset: search.offset } : {}),
  }),
  component: function Users() {
    const shown = usersRoute.useSearch();
    const navigate = useNavigate();
    const show = useCallback(
      (next: UsersShown, replace = false) => navigate({ to: "/admin/users", search: { q: next.q || undefined, offset: next.offset || undefined }, replace }),
      [navigate],
    );
    return <UsersPage shown={shown} show={show} />;
  },
});

export const adminTree = adminRoute.addChildren([
  adminIndexRoute,
  accessRoute,
  usersRoute,
  page("workspaces", s.admin.pages.workspaces, () => <WorkspacesSection />),
  page("unattended-writes", s.admin.pages.unattendedWrites, () => <UnattendedWritesSection />),
  page("providers", s.admin.pages.providers, () => <ProvidersSection />),
  page("traces", s.admin.pages.traces, () => <TraceDestinationsSection />),
  page("tools", s.admin.pages.tools, () => <ToolsSection />),
  page("assistants", s.admin.pages.assistants, () => <AllAssistantsSection />),
  createRoute({ getParentRoute: () => adminRoute, path: "audit", component: AuditLog }),
  createRoute({ getParentRoute: () => adminRoute, path: "usage", component: EveryonesUsagePage }),
  page("retention", s.admin.pages.retention, () => <RetentionSection />),
]);
