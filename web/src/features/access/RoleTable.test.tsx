import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { s } from "@/strings";
import { ACCESS_ANSWERS, ADMIN_GRANT, BOB, EVERYONE, grant } from "@/test/access";
import { renderAt } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const TRIAGE = { id: "0190a5c4-0000-7000-8000-00000000aa01", name: "triage" };
const BOB_EDITS = grant("a2", {
  role: "editor",
  roleName: "Editor",
  userId: BOB.id,
  userName: BOB.name,
  userEmail: BOB.email,
  workflowId: TRIAGE.id,
  workflowName: "triage",
});
const EVERYONE_AUTHORS = grant("a3", { role: "author", roleName: "Author", groupId: EVERYONE.id, groupName: "Everyone", groupBuiltin: "everyone" });
const LAST_ADMIN = "This is the last grant of Administrator. Give the role to somebody else before taking it from here.";

/** The grants as the server pages them, remembering what each request asked. */
function serveGrants(rows = [ADMIN_GRANT, BOB_EDITS, EVERYONE_AUTHORS], total = rows.length) {
  const asked: URLSearchParams[] = [];
  const answer = (request: Request) => {
    const query = new URL(request.url).searchParams;
    asked.push(query);
    const q = query.get("q") ?? "";
    const shown = rows.filter((row) => !q || [row.userName, row.groupName, row.workflowName].some((name) => name?.toLowerCase().includes(q)));
    return Response.json({ assignments: shown, total: q ? shown.length : total, limit: 50, offset: Number(query.get("offset") ?? 0) });
  };
  return { asked, answer };
}

const rows = () => [...document.querySelectorAll("[data-assignment]")].map((row) => row.getAttribute("data-grantee"));

describe("the Roles tab", () => {
  it("shows who holds which role where, and finds grants by search and by role", async () => {
    const grants = serveGrants();
    await renderAt("/admin/access", { answers: { ...ACCESS_ANSWERS, "/api/v1/role-assignments": grants.answer } });
    expect(await screen.findByRole("tab", { name: s.access.tabs.roles })).toHaveAttribute("aria-selected", "true");
    const panel = screen.getByRole("tabpanel", { name: s.access.tabs.roles });
    expect(panel).toHaveAttribute("data-access-tab", "roles");
    await waitFor(() => expect(rows()).toEqual([ADMIN_GRANT.userName, BOB.name, s.access.groups.everyone]));
    const bob = within(panel).getByRole("row", { name: /Bob Builder/ });
    expect(within(bob).getByText("Editor")).toBeInTheDocument();
    expect(within(bob).getByText("triage")).toBeInTheDocument();
    const everyone = within(panel).getByRole("row", { name: /Everyone/ });
    expect(within(everyone).getByText(s.access.grants.everyWorkflow)).toBeInTheDocument();
    expect(within(everyone).getByText(s.access.grants.group)).toBeInTheDocument();
    expect(within(panel).getByText("3 grants")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.type(within(panel).getByLabelText(s.access.grants.find), "bob");
    await waitFor(() => expect(rows()).toEqual([BOB.name]));
    expect(grants.asked.at(-1)?.get("q")).toBe("bob");
    await userEvent.clear(within(panel).getByLabelText(s.access.grants.find));
    await userEvent.type(within(panel).getByLabelText(s.access.grants.find), "nobody");
    expect(await within(panel).findByText(s.access.grants.noMatch)).toBeInTheDocument();
    expect(within(panel).queryByText(s.access.grants.empty)).toBeNull();

    await userEvent.selectOptions(within(panel).getByLabelText(s.access.grants.onlyRole), "Author");
    await waitFor(() => expect(grants.asked.at(-1)?.get("role")).toBe("author"));
  });

  it("shows one page and turns to the next", async () => {
    const grants = serveGrants([ADMIN_GRANT, BOB_EDITS], 120);
    await renderAt("/admin/access", { answers: { ...ACCESS_ANSWERS, "/api/v1/role-assignments": grants.answer } });
    expect(await screen.findByText("1 to 2 of 120 grants")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: s.access.pager.nextPage }));
    await waitFor(() => expect(grants.asked.at(-1)?.get("offset")).toBe("50"));
    expect(grants.asked.at(-1)?.get("limit")).toBe("50");
  });

  it("grants a role to a person over one workflow, and to Everyone over the installation", async () => {
    const sent: unknown[] = [];
    await renderAt("/admin/access", {
      answers: {
        ...ACCESS_ANSWERS,
        "/api/v1/workflows": { workflows: [{ ...TRIAGE }] },
        "/api/v1/people": (request: Request) => Response.json({ people: new URL(request.url).searchParams.get("q") === "bob" ? [BOB] : [] }),
        "POST /api/v1/role-assignments": async (request: Request) => {
          sent.push(await request.json());
          return Response.json({ assignment: BOB_EDITS }, { status: 201 });
        },
      },
    });
    const form = await screen.findByRole("form", { name: s.access.grants.grant });
    const who = within(form).getByLabelText(s.access.grants.whoLabel);
    expect(within(form).getByRole("button", { name: s.access.grants.grant })).toBeDisabled();
    await userEvent.type(within(form).getByLabelText(s.access.people.findFor(s.access.grants.whoLabel)), "bob");
    await within(who).findByRole("option", { name: BOB.name });
    await userEvent.selectOptions(who, BOB.name);
    await userEvent.selectOptions(within(form).getByLabelText(s.access.grants.role), "Editor");
    expect(within(form).getByText(/^Editor can see a workflow/)).toBeInTheDocument();
    await userEvent.selectOptions(await within(form).findByLabelText(s.access.grants.whereLabel), "triage");
    await userEvent.click(within(form).getByRole("button", { name: s.access.grants.grant }));
    await waitFor(() => expect(sent).toEqual([{ role: "editor", workflowId: TRIAGE.id, userId: BOB.id }]));

    await userEvent.selectOptions(who, s.access.groups.everyone);
    await userEvent.selectOptions(within(form).getByLabelText(s.access.grants.role), "Author");
    // Author is only ever over every workflow at once, so where it applies is not asked.
    expect(within(form).getByLabelText(s.access.grants.whereLabel)).toBeDisabled();
    await userEvent.click(within(form).getByRole("button", { name: s.access.grants.grant }));
    await waitFor(() => expect(sent.at(-1)).toEqual({ role: "author", groupId: EVERYONE.id }));
  });

  it("revokes a grant after asking, and says why the server refused one", async () => {
    let revoked = "";
    await renderAt("/admin/access", {
      answers: {
        ...ACCESS_ANSWERS,
        "/api/v1/role-assignments": { assignments: [ADMIN_GRANT, BOB_EDITS], total: 2, limit: 50, offset: 0 },
        [`DELETE /api/v1/role-assignments/${BOB_EDITS.id}`]: () => {
          revoked = BOB_EDITS.id;
          return new Response(null, { status: 204 });
        },
        [`DELETE /api/v1/role-assignments/${ADMIN_GRANT.id}`]: () => Response.json({ error: { code: "conflict", message: LAST_ADMIN } }, { status: 409 }),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: s.access.grants.revokeFrom("Editor", BOB.name) }));
    const dialog = screen.getByRole("dialog", { name: s.access.grants.revokeTitle });
    expect(within(dialog).getByText(s.access.grants.revokeBody("Editor"))).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(dialog).getByRole("button", { name: s.access.grants.revokeRole }));
    await waitFor(() => expect(revoked).toBe(BOB_EDITS.id));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());

    await userEvent.click(screen.getByRole("button", { name: s.access.grants.revokeFrom("Administrator", ADMIN_GRANT.userName ?? "") }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: s.access.grants.revokeRole }));
    expect(await within(screen.getByRole("dialog")).findByRole("alert")).toHaveTextContent(LAST_ADMIN);
  });
});
