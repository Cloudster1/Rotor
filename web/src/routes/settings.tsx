import { createRoute, redirect } from "@tanstack/react-router";
import { AssistantsSection } from "@/features/oauth/AssistantsSection";
import { AccountPage } from "@/features/settings/AccountPage";
import { SettingsLayout } from "@/features/settings/SettingsLayout";
import { SettingsPage } from "@/features/settings/ui";
import { MyUsagePage } from "@/features/usage/UsagePage";
import { s } from "@/strings";
import { shellRoute } from "./shell";

/** The person's own pages (#199), each a child route beside the side nav. */
export const settingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/settings",
  component: SettingsLayout,
});

const settingsIndexRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/settings/account", replace: true });
  },
});

const accountRoute = createRoute({ getParentRoute: () => settingsRoute, path: "account", component: AccountPage });

const assistantsRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "assistants",
  component: () => (
    <SettingsPage title={s.settings.pages.assistants}>
      <AssistantsSection />
    </SettingsPage>
  ),
});

const usageRoute = createRoute({ getParentRoute: () => settingsRoute, path: "usage", component: MyUsagePage });

// The audit log lived here before it moved to Admin; links to the old address still arrive.
const oldAuditRoute = createRoute({
  getParentRoute: () => settingsRoute,
  path: "audit",
  beforeLoad: () => {
    throw redirect({ to: "/admin/audit", replace: true });
  },
});

export const settingsTree = settingsRoute.addChildren([settingsIndexRoute, accountRoute, assistantsRoute, usageRoute, oldAuditRoute]);
