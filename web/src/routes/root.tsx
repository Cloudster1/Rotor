import { createRootRouteWithContext, Outlet, type ErrorComponentProps } from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { AppShell } from "@/features/shell/AppShell";
import { EmptyState, PageHeader } from "@/features/shell/EmptyState";
import { Icon } from "@/features/shell/icons";
import { s } from "@/strings";

export interface RouterContext {
  queryClient: QueryClient;
}

// The shell belongs to the layout route below the root, so a page of its own (sign-in, #6) can stand
// without it; a path that leads nowhere still gets the shell, so there is somewhere to go.
export const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  notFoundComponent: () => (
    <AppShell>
      <NotFound />
    </AppShell>
  ),
});

export function NotFound() {
  return (
    <div data-not-found>
      <PageHeader title={s.notFound.title} />
      <EmptyState icon={<Icon.Warning />} title={s.notFound.title} description={s.notFound.body} />
    </div>
  );
}

// A page that throws loses only itself: the shell stays, and the reader can try again.
export function RouteError({ error, reset }: ErrorComponentProps) {
  return (
    <div data-route-error>
      <PageHeader title={s.routeError.title} />
      <EmptyState
        icon={<Icon.Warning />}
        title={s.routeError.title}
        description={error instanceof Error && error.message ? error.message : s.routeError.fallback}
        action={
          <button
            type="button"
            onClick={reset}
            className="h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised"
          >
            {s.routeError.retry}
          </button>
        }
      />
    </div>
  );
}
