import { createRouter, type RouterHistory } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { approvalsRoute, connectionsRoute, indexRoute, runsRoute, settingsRoute, workflowsRoute } from "./places";
import { RouteError, rootRoute } from "./root";
import { shellRoute } from "./shell";

// Pages that stand without the shell, such as sign-in from #6, are children of rootRoute beside shellRoute.
const routeTree = rootRoute.addChildren([shellRoute.addChildren([indexRoute, workflowsRoute, runsRoute, approvalsRoute, connectionsRoute, settingsRoute])]);

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

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}
