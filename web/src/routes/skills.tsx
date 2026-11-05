import { createRoute, lazyRouteComponent, redirect } from "@tanstack/react-router";
import { shellRoute } from "./shell";

const listModule = () => import("@/features/skills/SkillsPage");
const pagesModule = () => import("@/features/skills/SkillPages");

export const skillsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/skills",
  component: lazyRouteComponent(listModule, "SkillsPage"),
});

export const newSkillRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/skills/new",
  component: lazyRouteComponent(() => import("@/features/skills/NewSkillPage"), "NewSkillPage"),
});

export const importSkillRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/skills/import",
  component: lazyRouteComponent(() => import("@/features/skills/ImportSkillPage"), "ImportSkillPage"),
});

/** One skill (#223): a side nav of its sections, each a child route, as a workflow's settings have. */
export const skillRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/skills/$skillID",
  component: lazyRouteComponent(pagesModule, "SkillLayout"),
});

const indexRoute = createRoute({
  getParentRoute: () => skillRoute,
  path: "/",
  beforeLoad: ({ params }) => {
    throw redirect({ to: "/skills/$skillID/files", params, replace: true });
  },
});

// An older version opens read-only from Versions, named in the address so a reload keeps it.
const filesRoute = createRoute({
  getParentRoute: () => skillRoute,
  path: "files",
  validateSearch: (search: Record<string, unknown>): { version?: number } =>
    typeof search.version === "number" && Number.isInteger(search.version) && search.version > 0 ? { version: search.version } : {},
  component: lazyRouteComponent(pagesModule, "FilesPage"),
});

function section<TPath extends string>(path: TPath, name: "VersionsPage" | "SharingPage" | "GeneralPage") {
  return createRoute({ getParentRoute: () => skillRoute, path, component: lazyRouteComponent(pagesModule, name) });
}

export const skillTree = skillRoute.addChildren([
  indexRoute,
  filesRoute,
  section("versions", "VersionsPage"),
  section("sharing", "SharingPage"),
  section("general", "GeneralPage"),
]);
