import { createRoute, redirect } from "@tanstack/react-router";
import { ConnectionsPage, connectionsSearch } from "@/features/connections/ConnectionsPage";
import { AssistantsSection } from "@/features/oauth/AssistantsSection";
import { ProfilePage } from "@/features/settings/ProfilePage";
import { SettingsLayout } from "@/features/settings/SettingsLayout";
import { ThemePage } from "@/features/settings/ThemePage";
import { SettingsPage } from "@/features/settings/ui";
import { MyUsagePage } from "@/features/usage/UsagePage";
import { s } from "@/strings";
import { shellRoute } from "./shell";

/** The person's own pages (#199, #206), each a child route beside the side nav. */
export const settingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings",
  component: SettingsLayout,
});

const settingsIndexRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/settings/profile", replace: true });
  },
});

const profileRoute = createRoute({ getParentRoute: () => settingsRoute, path: "profile", component: ProfilePage });

const themeRoute = createRoute({ getParentRoute: () => settingsRoute, path: "theme", component: ThemePage });

const assistantsRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "assistants",
  component: () => (
    <SettingsPage title={s.settings.pages.assistants} intro={s.settings.intros.assistants}>
      <AssistantsSection />
    </SettingsPage>
  ),
});

const usageRoute = createRoute({ getParentRoute: () => settingsRoute, path: "usage", component: MyUsagePage });

// The page a tool's connection callback lands on, with what the tool said in the query.
export const accountsRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "accounts",
  validateSearch: connectionsSearch,
  component: function AccountsRoute() {
    return <ConnectionsPage search={accountsRoute.useSearch()} />;
  },
});

// Profile was Account before #206, and the audit log lived here before it moved to Admin; old links still arrive.
const oldAccountRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "account",
  beforeLoad: () => {
    throw redirect({ to: "/settings/profile", replace: true });
  },
});

const oldAuditRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "audit",
  beforeLoad: () => {
    throw redirect({ to: "/admin/audit", replace: true });
  },
});

export const settingsTree = settingsRoute.addChildren([
  settingsIndexRoute,
  profileRoute,
  themeRoute,
  assistantsRoute,
  usageRoute,
  accountsRoute,
  oldAccountRoute,
  oldAuditRoute,
]);
