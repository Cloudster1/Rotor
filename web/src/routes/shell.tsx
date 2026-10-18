import { createRoute, Outlet } from "@tanstack/react-router";
import { AppShell } from "@/features/shell/AppShell";
import { rootRoute } from "./root";

/** Every page in the shell. #6 gives it a beforeLoad that sends a visitor without a session to sign in. */
export const shellRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "shell",
  component: () => (
    <AppShell>
      <Outlet />
    </AppShell>
  ),
});
