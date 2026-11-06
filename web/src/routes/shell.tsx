import { createRoute, Outlet, redirect } from "@tanstack/react-router";
import { ApiError } from "@/api/client";
import { meQuery } from "@/api/auth";
import { preferencesQuery } from "@/api/preferences";
import { LOGIN_PATH } from "@/config";
import { AccountControl } from "@/features/auth/AccountControl";
import { AppShell } from "@/features/shell/AppShell";
import { setTimeZone } from "@/lib/dates";
import { rootRoute } from "./root";

/** Every page in the shell, for signed-in people only: a visitor without a session is sent to sign in and back. */
export const shellRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "shell",
  beforeLoad: async ({ context, location }) => {
    try {
      await context.queryClient.ensureQueryData(meQuery);
    } catch (error) {
      if (error instanceof ApiError && error.isUnauthenticated) throw redirect({ to: LOGIN_PATH, search: { next: location.href } });
      throw error;
    }
    // Pages write dates in the person's zone from their first draw; an unanswered read leaves the browser's.
    const chosen = await context.queryClient.ensureQueryData(preferencesQuery).catch(() => null);
    setTimeZone(chosen?.timeZone);
  },
  component: () => (
    <AppShell topBarTrailing={<AccountControl />}>
      <Outlet />
    </AppShell>
  ),
});
