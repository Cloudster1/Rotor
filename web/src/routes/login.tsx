import { createRoute, redirect } from "@tanstack/react-router";
import { meQuery } from "@/api/auth";
import { HOME_PATH, LOGIN_PATH } from "@/config";
import { LoginPage } from "@/features/auth/LoginPage";
import { safeNext } from "@/lib/session";
import { rootRoute } from "./root";

/** What the sign-in page reads from its address: where to land afterwards, and why the last attempt failed. */
export interface LoginSearch {
  next?: string;
  error?: string;
}

/** The sign-in page stands without the shell. Somebody already signed in goes straight on. */
export const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: LOGIN_PATH,
  validateSearch: (search: Record<string, unknown>): LoginSearch => ({
    next: typeof search.next === "string" ? search.next : undefined,
    error: typeof search.error === "string" ? search.error : undefined,
  }),
  beforeLoad: async ({ context, search }) => {
    const me = await context.queryClient.fetchQuery(meQuery).catch(() => undefined);
    if (me) throw redirect({ href: safeNext(search.next) ?? HOME_PATH });
  },
  component: LoginRoute,
});

function LoginRoute() {
  const { next, error } = loginRoute.useSearch();
  return <LoginPage next={safeNext(next)} error={error} />;
}
