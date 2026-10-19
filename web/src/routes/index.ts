import { createRouter, type RouterHistory } from "@tanstack/react-router";
import { meQueryKey } from "@/api/auth";
import { LOGIN_PATH } from "@/config";
import { loginRoute } from "./login";
import type { QueryClient } from "@tanstack/react-query";
import { approvalRoute, approvalsRoute } from "./approvals";
import { auditRoute } from "./audit";
import { connectionsRoute, indexRoute, settingsRoute } from "./places";
import { newWorkflowRoute, workflowRoute, workflowsRoute } from "./workflows";
import { RouteError, rootRoute } from "./root";
import { runRoute, runsRoute } from "./runs";
import { shellRoute } from "./shell";
import { usageRoute } from "./usage";

// Pages that stand without the shell, such as sign-in, are children of rootRoute beside shellRoute.
const routeTree = rootRoute.addChildren([
  loginRoute,
  shellRoute.addChildren([
    indexRoute,
    workflowsRoute,
    newWorkflowRoute,
    workflowRoute,
    runsRoute,
    runRoute,
    approvalsRoute,
    approvalRoute,
    connectionsRoute,
    settingsRoute,
    auditRoute,
    usageRoute,
  ]),
]);

/** The application's router; tests pass a memory history to start anywhere. */
export function buildRouter(queryClient: QueryClient, history?: RouterHistory) {
  return createRouter({
    routeTree,
    history,
    context: { queryClient },
    defaultPreload: "intent",
    defaultErrorComponent: RouteError,
  });
}

export type AppRouter = ReturnType<typeof buildRouter>;

/** A 401 from any request means the session ended: forget who was signed in and sign in again, then come back. */
export function sendToLogin(router: AppRouter, client: QueryClient) {
  client.removeQueries({ queryKey: meQueryKey });
  const here = router.state.location;
  if (here.pathname === LOGIN_PATH) return;
  void router.navigate({ to: LOGIN_PATH, search: { next: here.href } });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}
