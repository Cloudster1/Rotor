import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { s } from "@/strings";
import { ME, renderAt } from "@/test/app";
import { axeViolations } from "@/test/axe";
import { BUILTIN, SKILL } from "@/test/skills";

beforeAll(async () => {
  await import("./SkillsPage");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const page = (skills: unknown[], total = skills.length) => ({ skills, total, limit: 50, offset: 0 });

describe("the Skills page", () => {
  it("is a place in the sidebar for everyone signed in, and says what a skill is when there is none", async () => {
    await renderAt("/skills", { me: { ...ME, role: "member" } });
    const nav = screen.getByRole("navigation", { name: s.shell.navigation });
    expect(within(nav).getByRole("link", { name: s.places.skills })).toHaveAttribute("aria-current", "page");
    const empty = await screen.findByRole("heading", { level: 2, name: s.skills.list.emptyTitle });
    const state = empty.closest("[data-empty-state]") as HTMLElement;
    expect(within(state).getByText(s.skills.list.emptyBody)).toBeInTheDocument();
    expect(within(state).getByRole("link", { name: s.skills.list.newSkill })).toHaveAttribute("href", "/skills/new");
    expect(within(state).getByRole("link", { name: s.skills.list.importZip })).toHaveAttribute("href", "/skills/import");
    expect(await axeViolations()).toEqual([]);
  });

  it("lists the skills the person reads, built-in ones marked and owned by Rotor", async () => {
    await renderAt("/skills", { answers: { "/api/v1/skills": page([BUILTIN, SKILL]) } });
    const builtin = (await screen.findByRole("link", { name: BUILTIN.name })).closest("li") as HTMLElement;
    expect(within(builtin).getByText(s.skills.list.builtin)).toBeInTheDocument();
    expect(within(builtin).getByText(new RegExp(s.skills.list.owner(s.skills.list.rotor)))).toBeInTheDocument();
    expect(within(builtin).getByText(BUILTIN.description)).toBeInTheDocument();
    const own = screen.getByRole("link", { name: SKILL.name }).closest("li") as HTMLElement;
    expect(within(own).queryByText(s.skills.list.builtin)).toBeNull();
    expect(within(own).getByText(new RegExp(s.skills.list.version(2)))).toBeInTheDocument();
    expect(screen.getByRole("link", { name: SKILL.name })).toHaveAttribute("href", `/skills/${SKILL.id}/files`);
    expect(await axeViolations()).toEqual([]);
  });

  it("asks for the archived ones behind the toggle, and searches by q", async () => {
    const asked: string[] = [];
    await renderAt("/skills", {
      answers: {
        "/api/v1/skills": (request: Request) => {
          const query = new URL(request.url).searchParams;
          asked.push(query.toString());
          if (query.get("archived") === "true") return Response.json(page([]));
          return Response.json(page(query.get("q") ? [] : [SKILL]));
        },
      },
    });
    await screen.findByRole("link", { name: SKILL.name });
    await userEvent.click(screen.getByRole("button", { name: s.skills.list.showArchived }));
    expect(await screen.findByRole("heading", { level: 2, name: s.skills.list.archivedTitle })).toBeInTheDocument();
    expect(asked.some((q) => q.includes("archived=true"))).toBe(true);
    await userEvent.click(screen.getByRole("button", { name: s.skills.list.showActive }));
    await userEvent.type(screen.getByLabelText(s.skills.list.search), "nothing");
    expect(await screen.findByText(s.skills.list.noMatch("nothing"))).toBeInTheDocument();
    await waitFor(() => expect(asked.some((q) => q.includes("q=nothing"))).toBe(true));
  });

  it("pages a list longer than one page", async () => {
    const many = Array.from({ length: 50 }, (_, i) => ({ ...SKILL, id: `${SKILL.id.slice(0, -2)}${String(i).padStart(2, "0")}`, name: `skill-${i}` }));
    const offsets: string[] = [];
    await renderAt("/skills", {
      answers: {
        "/api/v1/skills": (request: Request) => {
          offsets.push(new URL(request.url).searchParams.get("offset") ?? "0");
          return Response.json({ ...page(many, 60), offset: Number(new URL(request.url).searchParams.get("offset") ?? 0) });
        },
      },
    });
    const pager = await screen.findByRole("navigation", { name: s.skills.list.pages });
    await userEvent.click(within(pager).getByRole("button", { name: s.access.pager.nextPage }));
    await waitFor(() => expect(offsets).toContain("50"));
  });
});
