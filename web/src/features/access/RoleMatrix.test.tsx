import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Role } from "@/api/access";
import { s } from "@/strings";
import { ACCESS_ANSWERS, AUDITOR, ROLES } from "@/test/access";
import { renderAt } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

/** The roles as a server keeps them: a change is seen by the next listing. */
function serveRoles() {
  let roles: Role[] = [...ROLES, AUDITOR];
  const sent: { method: string; key?: string; body?: unknown }[] = [];
  const answers = {
    ...ACCESS_ANSWERS,
    "/api/v1/roles": () => Response.json({ roles }),
    "POST /api/v1/roles": async (request: Request) => {
      const body = (await request.json()) as Role;
      sent.push({ method: "POST", body });
      const made = {
        ...body,
        key: "release_manager",
        builtin: false,
        inUse: 0,
        description: body.description ?? "",
        installationWideOnly: Boolean(body.installationWideOnly),
      };
      roles = [...roles, made];
      return Response.json({ role: made }, { status: 201 });
    },
  } as Record<string, unknown>;
  for (const role of [...ROLES, AUDITOR]) {
    answers[`PATCH /api/v1/roles/${role.key}`] = async (request: Request) => {
      const body = (await request.json()) as Partial<Role>;
      sent.push({ method: "PATCH", key: role.key, body });
      roles = roles.map((each) => (each.key === role.key ? { ...each, ...body } : each));
      return Response.json({ role: roles.find((each) => each.key === role.key) });
    };
  }
  answers[`DELETE /api/v1/roles/${AUDITOR.key}`] = () => {
    sent.push({ method: "DELETE", key: AUDITOR.key });
    roles = roles.filter((each) => each.key !== AUDITOR.key);
    return new Response(null, { status: 204 });
  };
  return { answers, sent };
}

const cell = (role: string, permission: string) => screen.getByLabelText(s.access.matrix.may(role, permission)) as HTMLInputElement;

describe("the role matrix", () => {
  it("draws a box at every crossing and saves the whole list the moment one is ticked", async () => {
    const server = serveRoles();
    await renderAt("/admin/access?tab=matrix", { answers: server.answers });
    expect(await screen.findByRole("tabpanel", { name: s.access.tabs.matrix })).toHaveAttribute("data-access-tab", "matrix");
    await screen.findByLabelText(s.access.matrix.may("Runner", "save new versions of a workflow"));
    expect(cell("Runner", "save new versions of a workflow").checked).toBe(false);
    await userEvent.click(cell("Runner", "save new versions of a workflow"));
    await waitFor(() =>
      expect(server.sent).toEqual([{ method: "PATCH", key: "runner", body: { permissions: ["workflow.read", "workflow.run", "workflow.edit"] } }]),
    );
    await waitFor(() => expect(cell("Runner", "save new versions of a workflow").checked).toBe(true));

    await userEvent.click(cell("Editor", "start runs of a workflow and follow their own"));
    await waitFor(() => expect(server.sent.at(-1)).toEqual({ method: "PATCH", key: "editor", body: { permissions: ["workflow.read", "workflow.edit"] } }));
    expect(await axeViolations()).toEqual([]);
  });

  it("never lets the administrator stop administering", async () => {
    await renderAt("/admin/access?tab=matrix", { answers: serveRoles().answers });
    await screen.findByLabelText(s.access.matrix.may("Administrator", "administer Rotor"));
    expect(cell("Administrator", "administer Rotor").checked).toBe(true);
    expect(cell("Administrator", "administer Rotor").disabled).toBe(true);
    expect(cell("Administrator", "create workflows").disabled).toBe(false);
  });

  it("deletes a role somebody added after naming its grants, and never a built-in one", async () => {
    const server = serveRoles();
    await renderAt("/admin/access?tab=matrix", { answers: server.answers });
    const head = (await screen.findByText("Auditor")).closest("th") as HTMLElement;
    expect(within(head).queryByText(s.access.matrix.builtIn)).toBeNull();
    await userEvent.click(within(head).getByRole("button", { name: s.access.matrix.actionsFor("Auditor") }));
    await userEvent.click(within(head).getByRole("button", { name: s.access.delete }));
    const dialog = screen.getByRole("dialog", { name: s.access.matrix.deleteTitle("Auditor") });
    expect(within(dialog).getByText(s.access.matrix.deleteBody("Auditor", 3))).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: s.access.matrix.deleteRole }));
    await waitFor(() => expect(server.sent).toEqual([{ method: "DELETE", key: "auditor" }]));
    expect(await screen.findByRole("status")).toHaveTextContent(s.access.matrix.deleted("Auditor"));
    await waitFor(() => expect(screen.queryByText("Auditor")).toBeNull());

    const runner = screen.getByText("Runner").closest("th") as HTMLElement;
    expect(within(runner).getByText(s.access.matrix.builtIn)).toBeInTheDocument();
    await userEvent.click(within(runner).getByRole("button", { name: s.access.matrix.actionsFor("Runner") }));
    expect(within(runner).getByRole("button", { name: s.access.delete })).toBeDisabled();
  });

  it("adds a role that starts with seeing workflows, and renames one", async () => {
    const server = serveRoles();
    await renderAt("/admin/access?tab=matrix", { answers: server.answers });
    await userEvent.click(await screen.findByRole("button", { name: s.access.matrix.newRole }));
    const dialog = screen.getByRole("dialog", { name: s.access.matrix.newRole });
    expect(await axeViolations()).toEqual([]);
    await userEvent.type(within(dialog).getByLabelText(s.access.matrix.name), "Release manager");
    await userEvent.type(within(dialog).getByLabelText(s.access.matrix.purpose), "Ships releases.");
    await userEvent.click(within(dialog).getByRole("button", { name: s.access.matrix.addRole }));
    await waitFor(() =>
      expect(server.sent).toEqual([
        { method: "POST", body: { name: "Release manager", description: "Ships releases.", installationWideOnly: false, permissions: ["workflow.read"] } },
      ]),
    );
    expect(await screen.findByRole("status")).toHaveTextContent(s.access.matrix.added("Release manager"));
    expect((await screen.findByLabelText(s.access.matrix.may("Release manager", "create workflows"))) as HTMLInputElement).not.toBeChecked();

    await userEvent.click(screen.getByRole("button", { name: s.access.matrix.actionsFor("Runner") }));
    await userEvent.click(screen.getByRole("button", { name: s.access.matrix.rename }));
    const rename = screen.getByRole("dialog", { name: s.access.matrix.renameTitle("Runner") });
    await userEvent.clear(within(rename).getByLabelText(s.access.matrix.name));
    await userEvent.type(within(rename).getByLabelText(s.access.matrix.name), "Starter");
    await userEvent.click(within(rename).getByRole("button", { name: s.access.save }));
    await waitFor(() =>
      expect(server.sent.at(-1)).toEqual({ method: "PATCH", key: "runner", body: { name: "Starter", description: "Runner of the test installation." } }),
    );
    expect(await screen.findByText("Starter")).toBeInTheDocument();
  });

  it("moves between the tabs with the arrow keys, and keeps the tab in the address", async () => {
    const { router } = await renderAt("/admin/access", { answers: ACCESS_ANSWERS });
    const roles = await screen.findByRole("tab", { name: s.access.tabs.roles });
    roles.focus();
    await userEvent.keyboard("{ArrowRight}");
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "matrix" }));
    expect(screen.getByRole("tab", { name: s.access.tabs.matrix })).toHaveFocus();
    await userEvent.keyboard("{End}");
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "sso" }));
    expect(screen.getByRole("tabpanel", { name: s.access.tabs.sso })).toBeVisible();
    await userEvent.click(screen.getByRole("tab", { name: s.access.tabs.roles }));
    await waitFor(() => expect(router.state.location.search).toEqual({}));
  });
});
