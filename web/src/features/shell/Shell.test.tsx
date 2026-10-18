import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { META, renderAt } from "@/test/app";
import { PLACES } from "./places";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const EMPTY: Record<string, string> = {
  "/workflows": s.workflows.emptyTitle,
  "/runs": s.runs.emptyTitle,
  "/approvals": s.approvals.emptyTitle,
  "/connections": s.connections.emptyTitle,
};

describe("the shell", () => {
  it("sends the index to the first place", async () => {
    const { router } = await renderAt("/");
    await waitFor(() => expect(router.state.location.pathname).toBe("/workflows"));
    expect(await screen.findByRole("heading", { level: 1, name: s.places.workflows })).toBeInTheDocument();
  });

  it.each(Object.entries(EMPTY))("draws %s with its heading and empty state", async (path, emptyTitle) => {
    await renderAt(path);
    const place = PLACES.find((each) => each.path === path)!;
    expect(await screen.findByRole("heading", { level: 1, name: place.label })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: emptyTitle })).toBeInTheDocument();
  });

  it("marks the page open in the sidebar, and only that one", async () => {
    await renderAt("/runs");
    const nav = await screen.findByRole("navigation", { name: s.shell.navigation });
    const current = within(nav)
      .getAllByRole("link")
      .filter((link) => link.getAttribute("aria-current") === "page");
    expect(current.map((link) => link.textContent)).toEqual([s.places.runs]);
  });

  it("follows the sidebar to another place", async () => {
    const { router } = await renderAt("/workflows");
    const nav = await screen.findByRole("navigation", { name: s.shell.navigation });
    await userEvent.click(within(nav).getByRole("link", { name: s.places.approvals }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/approvals"));
    expect(await screen.findByRole("heading", { level: 2, name: s.approvals.emptyTitle })).toBeInTheDocument();
  });

  it("shows the server's build in the settings", async () => {
    await renderAt("/settings");
    expect(await screen.findByText(META.build.version)).toBeInTheDocument();
    expect(screen.getByText(META.apiVersion)).toBeInTheDocument();
  });

  it("puts a path that leads nowhere inside the shell", async () => {
    await renderAt("/nowhere");
    expect(await screen.findByRole("heading", { level: 1, name: s.notFound.title })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: s.shell.navigation })).toBeInTheDocument();
  });

  it.each([...PLACES.map((place) => place.path), "/nowhere"])("passes axe at %s", async (path) => {
    await renderAt(path);
    await screen.findByRole("heading", { level: 1 });
    expect(await axeViolations()).toEqual([]);
  });
});
