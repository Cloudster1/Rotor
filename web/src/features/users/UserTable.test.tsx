import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ManagedUser } from "@/api/users";
import { s } from "@/strings";
import { ACCESS_ANSWERS, BOB, CAROL, ENGINEERING, user } from "@/test/access";
import { ME, renderAt } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const ADA = user({ ...ME.user }, { grants: [{ role: "administrator", roleName: "Administrator" }] });
const BOBS = user(BOB, {
  groups: [
    { id: "e", name: "Everyone", source: "local", builtin: "everyone" },
    { id: ENGINEERING.id, name: "engineering", source: "oidc", builtin: "" },
  ],
  grants: [
    { role: "editor", roleName: "Editor", workflowId: "w1", workflowName: "triage" },
    { role: "runner", roleName: "Runner", workflowId: "w2", workflowName: "release", group: "engineering" },
  ],
});
const LAST_ADMIN = "Ada Admin is the last active administrator. Make somebody else an administrator first.";

/** The people as a server keeps them, switched off and on by PATCH; Ada is the last administrator. */
function servePeople(total?: number) {
  let users: ManagedUser[] = [ADA, BOBS, user(CAROL)];
  const asked: URLSearchParams[] = [];
  const sent: { id: string; isActive: boolean }[] = [];
  const answers: Record<string, unknown> = {
    ...ACCESS_ANSWERS,
    "/api/v1/users": (request: Request) => {
      const query = new URL(request.url).searchParams;
      asked.push(query);
      const q = query.get("q") ?? "";
      const shown = users.filter((each) => !q || each.name.toLowerCase().includes(q));
      return Response.json({ users: shown, total: total ?? shown.length, limit: 50, offset: Number(query.get("offset") ?? 0) });
    },
  };
  for (const each of users) {
    answers[`PATCH /api/v1/users/${each.id}`] = async (request: Request) => {
      const { isActive } = (await request.json()) as { isActive: boolean };
      sent.push({ id: each.id, isActive });
      if (each.id === ADA.id) return Response.json({ error: { code: "conflict", message: LAST_ADMIN } }, { status: 409 });
      users = users.map((one) => (one.id === each.id ? { ...one, isActive } : one));
      return Response.json({ user: users.find((one) => one.id === each.id) });
    };
  }
  return { answers, asked, sent };
}

const row = (email: string) => document.querySelector(`[data-user="${email}"]`) as HTMLElement;

describe("the Users page", () => {
  it("lists everybody with their groups and roles, on which workflow and through which group", async () => {
    await renderAt("/admin/users", { answers: servePeople().answers });
    expect(await screen.findByRole("heading", { level: 1, name: s.users.title })).toBeInTheDocument();
    await waitFor(() => expect(row(BOB.email)).not.toBeNull());
    const bob = within(row(BOB.email));
    expect(bob.getByText("Everyone")).toBeInTheDocument();
    expect(bob.getByText("engineering")).toBeInTheDocument();
    expect(bob.getByText("Editor on triage")).toBeInTheDocument();
    expect(bob.getByText("Runner on release through engineering")).toBeInTheDocument();
    expect(bob.getByText(s.users.active)).toBeInTheDocument();
    expect(within(row(CAROL.email)).getAllByText(s.users.none)).toHaveLength(1);
    expect(within(row(ME.user.email)).getByText(`(${s.users.you})`)).toBeInTheDocument();
    expect(screen.getByText("3 accounts")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("switches somebody off after saying what it ends, and on again", async () => {
    const server = servePeople();
    await renderAt("/admin/users", { answers: server.answers });
    await userEvent.click(await screen.findByRole("button", { name: s.users.switchOffNamed(BOB.name) }));
    const dialog = screen.getByRole("dialog", { name: s.users.switchOffTitle(BOB.name) });
    expect(within(dialog).getByText(s.users.switchOffBody(BOB.name))).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: s.users.switchOff }));
    await waitFor(() => expect(server.sent).toEqual([{ id: BOB.id, isActive: false }]));
    expect(await screen.findByText(s.users.switchedOff(BOB.name))).toBeInTheDocument();
    await waitFor(() => expect(within(row(BOB.email)).getByText(s.users.off)).toBeInTheDocument());

    await userEvent.click(within(row(BOB.email)).getByRole("button", { name: s.users.switchOnNamed(BOB.name) }));
    await waitFor(() => expect(server.sent.at(-1)).toEqual({ id: BOB.id, isActive: true }));
    expect(await screen.findByText(s.users.switchedOn(BOB.name))).toBeInTheDocument();
    await waitFor(() => expect(row(BOB.email)).toHaveAttribute("data-user-active", "true"));
  });

  it("says why the server would not switch the last administrator off", async () => {
    await renderAt("/admin/users", { answers: servePeople().answers });
    await userEvent.click(await screen.findByRole("button", { name: s.users.switchOffNamed(ME.user.name) }));
    const dialog = screen.getByRole("dialog");
    await userEvent.click(within(dialog).getByRole("button", { name: s.users.switchOff }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(LAST_ADMIN);
    expect(row(ME.user.email)).toHaveAttribute("data-user-active", "true");
  });

  it("finds people by search, kept in the address, and turns the page", async () => {
    const server = servePeople(120);
    const { router } = await renderAt("/admin/users", { answers: server.answers });
    await userEvent.click(await screen.findByRole("button", { name: s.access.pager.nextPage }));
    await waitFor(() => expect(server.asked.at(-1)?.get("offset")).toBe("50"));
    expect(router.state.location.search).toEqual({ offset: 50 });

    await userEvent.type(screen.getByLabelText(s.users.find), "carol");
    await waitFor(() => expect(router.state.location.search).toEqual({ q: "carol" }));
    await waitFor(() => expect(server.asked.at(-1)?.get("q")).toBe("carol"));
    expect(server.asked.at(-1)?.get("offset")).toBeNull();
  });
});
