import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { s } from "@/strings";
import { AXE_TIMEOUT_MS, axeViolations } from "@/test/axe";
import { ACCESS_ANSWERS } from "@/test/access";
import { ME, renderAt } from "@/test/app";
import { ADMIN_GROUPS, FIRST_ADMIN_PATH, SETTINGS_GROUPS } from "./groups";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const MEMBER = { ...ME, role: "member", groups: [] };
const DAY = { day: "2026-10-19", inputTokens: 0, outputTokens: 0, cost: 0 };

// What the pages ask beyond renderAt's defaults, so every one of them draws in full.
const ANSWERS = {
  ...ACCESS_ANSWERS,
  "/api/v1/usage": { today: DAY, month: DAY, days: [], limits: { dailyTokens: null, dailyCost: null, concurrentRuns: null } },
  "/api/v1/usage/people": { people: [] },
  "/api/v1/audit/facets": { actions: [], actors: [], targetTypes: [] },
  "/api/v1/audit": { entries: [], next: null },
};

const SETTINGS_PAGES = SETTINGS_GROUPS.flatMap((group) => group.pages);
const ADMIN_PAGES = ADMIN_GROUPS.flatMap((group) => group.pages);

function sidebar() {
  return screen.getByRole("navigation", { name: s.shell.navigation });
}

describe("settings and admin", () => {
  it("open Settings on the profile page", async () => {
    const { router } = await renderAt("/settings", { me: MEMBER });
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/profile"));
    expect(await screen.findByRole("heading", { level: 1, name: s.settings.pages.profile })).toBeInTheDocument();
  });

  it("send the old account address to the profile", async () => {
    const { router } = await renderAt("/settings/profile", { me: MEMBER });
    await waitFor(() => expect(router.state.location.pathname).toBe("/settings/profile"));
  });

  it.each(SETTINGS_PAGES.map((page) => [page.path, page.label] as const))(
    "open %s under its heading with a sentence on what it is for",
    async (path, label) => {
      await renderAt(path, { me: MEMBER, answers: ANSWERS });
      const heading = await screen.findByRole("heading", { level: 1, name: label });
      const intro = await screen.findByText((_, element) => element?.hasAttribute("data-page-intro") ?? false);
      expect(intro.textContent).toMatch(/\.$/);
      expect(heading.compareDocumentPosition(intro) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    },
  );

  it("open Admin on Access, the first page of People and access", async () => {
    const { router } = await renderAt("/admin", { answers: ANSWERS });
    await waitFor(() => expect(router.state.location.pathname).toBe("/admin/access"));
    expect(FIRST_ADMIN_PATH).toBe("/admin/access");
    expect(await screen.findByRole("heading", { level: 1, name: s.access.title })).toBeInTheDocument();
    const nav = screen.getByRole("navigation", { name: s.admin.nav });
    const first = within(nav).getAllByRole("list")[0] as HTMLElement;
    expect(first).toHaveAccessibleName(s.admin.groups.people);
    expect(
      within(first)
        .getAllByRole("link")
        .map((link) => link.getAttribute("href")),
    ).toEqual(["/admin/access", "/admin/users"]);
  });

  it("send the old audit address to Admin", async () => {
    const { router } = await renderAt("/settings/audit", { answers: ANSWERS });
    await waitFor(() => expect(router.state.location.pathname).toBe("/admin/audit"));
  });

  it("show a member their own pages and no Admin", async () => {
    await renderAt("/settings/profile", { me: MEMBER });
    const nav = await screen.findByRole("navigation", { name: s.settings.nav });
    expect(
      within(nav)
        .getAllByRole("link")
        .map((link) => link.textContent),
    ).toEqual(SETTINGS_PAGES.map((page) => page.label));
    expect(within(sidebar()).queryByRole("link", { name: s.places.admin })).not.toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: s.admin.nav })).not.toBeInTheDocument();
  });

  it("refuse a member every Admin page once, without its nav", async () => {
    for (const page of ADMIN_PAGES) {
      await renderAt(page.path, { me: MEMBER, answers: ANSWERS });
      expect(await screen.findByRole("alert")).toHaveTextContent(s.admin.notAllowed);
      expect(screen.getAllByRole("alert")).toHaveLength(1);
      expect(screen.queryByRole("navigation", { name: s.admin.nav })).not.toBeInTheDocument();
      cleanup();
    }
  });

  it("show an administrator Admin in the sidebar, its pages grouped by section", async () => {
    await renderAt("/settings/profile");
    expect(within(sidebar()).getByRole("link", { name: s.places.admin })).toHaveAttribute("href", "/admin");
    await userEvent.click(within(sidebar()).getByRole("link", { name: s.places.admin }));
    const nav = await screen.findByRole("navigation", { name: s.admin.nav });
    for (const group of ADMIN_GROUPS) {
      const list = within(nav).getByRole("list", { name: group.title });
      expect(
        within(list)
          .getAllByRole("link")
          .map((link) => link.textContent),
      ).toEqual(group.pages.map((page) => page.label));
    }
  });

  it("mark the page open in the side nav, and only that one", async () => {
    const { router } = await renderAt("/admin/tools");
    const nav = await screen.findByRole("navigation", { name: s.admin.nav });
    const current = () =>
      within(nav)
        .getAllByRole("link")
        .filter((link) => link.getAttribute("aria-current") === "page");
    expect(current().map((link) => link.getAttribute("data-settings-page"))).toEqual(["tools"]);
    await userEvent.click(within(nav).getByRole("link", { name: s.admin.pages.retention }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/admin/retention"));
    await waitFor(() => expect(current().map((link) => link.getAttribute("data-settings-page"))).toEqual(["retention"]));
    expect(within(sidebar()).getByRole("link", { name: s.places.admin })).toHaveAttribute("aria-current", "page");
  });

  it.each([...SETTINGS_PAGES, ...ADMIN_PAGES].map((page) => page.path))(
    "pass axe at %s, with one heading of level 1",
    async (path) => {
      await renderAt(path, { answers: ANSWERS });
      await screen.findByRole("heading", { level: 1 });
      expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
      expect(await axeViolations()).toEqual([]);
    },
    AXE_TIMEOUT_MS,
  );
});
