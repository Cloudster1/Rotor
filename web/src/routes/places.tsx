import { createRoute, redirect } from "@tanstack/react-router";
import { HOME_PATH } from "@/config";
import { connectionsSearch } from "@/features/connections/ConnectionsPage";
import { shellRoute } from "./shell";

/** The index has nothing of its own; it is the first place of the sidebar. */
export const indexRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: HOME_PATH });
  },
});

// Connections moved under Settings as Tool accounts (#206); old links and bookmarks still arrive here, with what a
// tool's callback said in the query, which goes along.
export const connectionsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/connections",
  validateSearch: connectionsSearch,
  beforeLoad: ({ search }) => {
    throw redirect({ to: "/settings/accounts", search, replace: true });
  },
});
