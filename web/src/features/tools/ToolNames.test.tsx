import { afterEach, describe, expect, it } from "vitest";
import { cleanup, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Tool } from "@/api/tools";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { renderAt } from "@/test/app";

afterEach(cleanup);

const EXCITER: Tool = {
  id: "0190a5c4-0000-7000-8000-0000000000e1",
  name: "exciter",
  product: "exciter",
  baseURL: "https://exciter.test",
  resource: "https://exciter.test/api/v1/mcp",
  mcpURL: "https://exciter.test/api/v1/mcp",
  hookURL: "https://rotor.test/api/v1/hooks/0190a5c4-0000-7000-8000-0000000000e1",
  issuer: "https://exciter.test",
  capabilities: ["mcp@1"],
  missing: [],
  scopesSupported: ["mcp", "mcp:read"],
  registration: "dynamic",
  status: "ready",
  addedAt: "2026-10-18T10:00:00Z",
};
const BINDER = { id: "0190a5c4-0000-7000-8000-0000000000f1", name: "Refine stories", ownerName: "Bob Builder" };
const TAKEN = "A tool called exciter exists already, and workflows bind tools by name. Give this one another name, such as exciter-2.";

describe("a tool's name (#267)", () => {
  it("is changed after the workflows binding it are listed, and those are named again once it is", async () => {
    let tools: Tool[] = [EXCITER];
    let sent: unknown;
    await renderAt("/admin/tools", {
      answers: {
        "/api/v1/tools": () => Response.json({ tools }),
        [`/api/v1/tools/${EXCITER.id}/workflows`]: { workflows: [BINDER] },
        [`PUT /api/v1/tools/${EXCITER.id}/name`]: async (request: Request) => {
          sent = await request.json();
          tools = [{ ...EXCITER, name: "exciter-prod" }];
          return Response.json({ tool: tools[0], workflows: [BINDER] });
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: s.toolNames.rename }));
    const form = screen.getByRole("form", { name: s.toolNames.renameLabel("exciter") });
    expect(await within(form).findByText(s.toolNames.binders("exciter"))).toBeInTheDocument();
    expect(within(form).getByRole("link", { name: BINDER.name })).toHaveAttribute("href", `/workflows/${BINDER.id}`);
    expect(await axeViolations()).toEqual([]);

    const name = within(form).getByLabelText(s.toolNames.renameLabel("exciter"));
    await userEvent.clear(name);
    await userEvent.type(name, "exciter-prod");
    await userEvent.click(within(form).getByRole("button", { name: s.toolNames.save }));
    const done = await screen.findByRole("status");
    expect(done).toHaveTextContent(s.toolNames.renamed("exciter", "exciter-prod"));
    expect(done).toHaveTextContent(s.toolNames.stillBound("exciter"));
    expect(sent).toEqual({ name: "exciter-prod" });
    expect(await screen.findByText("Exciter-prod")).toBeInTheDocument();
  });

  it("says when no workflow binds it, and shows a name the server refuses as it words it", async () => {
    await renderAt("/admin/tools", {
      answers: {
        "/api/v1/tools": { tools: [EXCITER] },
        [`/api/v1/tools/${EXCITER.id}/workflows`]: { workflows: [] },
        [`PUT /api/v1/tools/${EXCITER.id}/name`]: () =>
          Response.json({ error: { code: "tool_name_taken", message: TAKEN, fields: { name: TAKEN } } }, { status: 409 }),
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: s.toolNames.rename }));
    const form = screen.getByRole("form", { name: s.toolNames.renameLabel("exciter") });
    expect(await within(form).findByText(s.toolNames.noBinders("exciter"))).toBeInTheDocument();
    await userEvent.type(within(form).getByLabelText(s.toolNames.renameLabel("exciter")), "-2");
    await userEvent.click(within(form).getByRole("button", { name: s.toolNames.save }));
    expect(await within(form).findByRole("alert")).toHaveTextContent(TAKEN);
  });

  it("is given when adding, and a name another tool has is refused in the server's words", async () => {
    let sent: unknown;
    await renderAt("/admin/tools", {
      answers: {
        "/api/v1/tools": { tools: [EXCITER] },
        "POST /api/v1/tools": async (request: Request) => {
          sent = await request.json();
          return Response.json({ error: { code: "tool_name_taken", message: TAKEN, fields: { name: TAKEN } } }, { status: 409 });
        },
      },
    });
    await userEvent.type(await screen.findByLabelText(s.tools.add.baseURL), "https://exciter-two.test");
    await userEvent.type(screen.getByLabelText(s.toolNames.name), "exciter");
    await userEvent.click(screen.getByRole("button", { name: s.tools.add.submit }));
    expect(await screen.findByText(TAKEN)).toBeInTheDocument();
    expect(sent).toEqual({ baseURL: "https://exciter-two.test", name: "exciter" });
  });

  it("is listed when several tools share it from before, with each tool's product", async () => {
    const other = { ...EXCITER, id: "0190a5c4-0000-7000-8000-0000000000e2", baseURL: "https://exciter-old.test" };
    await renderAt("/admin/tools", { answers: { "/api/v1/tools": { tools: [EXCITER, other] } } });
    const warning = await screen.findByText(s.toolNames.shared);
    expect(within(warning.parentElement as HTMLElement).getByText(s.toolNames.sharedBy("exciter", 2))).toBeInTheDocument();
    expect(screen.getAllByText(s.toolNames.product)).toHaveLength(2);
    expect(await axeViolations()).toEqual([]);
  });
});
