import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Connection } from "@/api/connections";
import type { Tool } from "@/api/tools";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const EXCITER: Tool = {
  id: "0190a5c4-0000-7000-8000-0000000000e1",
  name: "exciter",
  baseURL: "https://exciter.test",
  resource: "https://exciter.test/api/v1/mcp",
  issuer: "https://exciter.test",
  capabilities: ["mcp@1"],
  scopesSupported: ["mcp", "mcp:read"],
  registration: "dynamic",
  status: "ready",
  addedAt: "2026-10-18T10:00:00Z",
};
const STATOR: Tool = { ...EXCITER, id: "0190a5c4-0000-7000-8000-0000000000e2", name: "stator", baseURL: "https://stator.test" };

const CONNECTED: Connection = {
  tool: { id: EXCITER.id, name: EXCITER.name, baseURL: EXCITER.baseURL },
  scopes: ["mcp", "mcp:read"],
  status: "connected",
  connectedAt: "2026-10-18T10:05:00Z",
  lastUsedAt: null,
};

describe("the connections", () => {
  it("say there is nothing to connect until a tool is added", async () => {
    await renderAt("/connections");
    expect(await screen.findByRole("heading", { level: 2, name: s.connections.emptyTitle })).toBeInTheDocument();
  });

  it("show each tool with what was allowed, and send the browser to consent at one not connected", async () => {
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign });
    await renderAt("/connections", {
      answers: {
        "/api/v1/tools": { tools: [EXCITER, STATOR] },
        "/api/v1/connections": { connections: [CONNECTED] },
        [`POST /api/v1/tools/${STATOR.id}/connection`]: { authorizeURL: "https://stator.test/oauth/authorize?state=s" },
      },
    });
    const exciter = await screen.findByRole("listitem", { name: "Exciter" });
    expect(within(exciter).getByText("mcp mcp:read")).toBeInTheDocument();
    expect(within(exciter).getByText(s.connections.never)).toBeInTheDocument();
    expect(within(exciter).queryByRole("button", { name: s.connections.connect("Exciter") })).not.toBeInTheDocument();

    const stator = screen.getByRole("listitem", { name: "Stator" });
    expect(within(stator).getByText(s.connections.status.none)).toBeInTheDocument();
    await userEvent.click(within(stator).getByRole("button", { name: s.connections.connect("Stator") }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith("https://stator.test/oauth/authorize?state=s"));
  });

  it("say what came back from the tool", async () => {
    await renderAt(`/connections?connected=${EXCITER.id}`, {
      answers: { "/api/v1/tools": { tools: [EXCITER] }, "/api/v1/connections": { connections: [CONNECTED] } },
    });
    expect(await screen.findByRole("status")).toHaveTextContent(s.connections.done("Exciter"));
    cleanup();
    await renderAt("/connections?error=denied", { answers: { "/api/v1/tools": { tools: [EXCITER] } } });
    expect(await screen.findByRole("alert")).toHaveTextContent(s.connections.errors.denied);
  });

  it("end after a confirmation, and offer to connect again one the tool ended", async () => {
    let connections: Connection[] = [CONNECTED, { ...CONNECTED, tool: { ...CONNECTED.tool, id: STATOR.id, name: "stator" }, status: "reconnect" }];
    const { fetch } = await renderAt("/connections", {
      answers: {
        "/api/v1/tools": { tools: [EXCITER, STATOR] },
        "/api/v1/connections": () => Response.json({ connections }),
        [`DELETE /api/v1/tools/${EXCITER.id}/connection`]: () => {
          connections = connections.filter((c) => c.tool.id !== EXCITER.id);
          return new Response(null, { status: 204 });
        },
      },
    });
    const stator = await screen.findByRole("listitem", { name: "Stator" });
    expect(within(stator).getByRole("button", { name: s.connections.connectAgain("Stator") })).toBeInTheDocument();

    const exciter = screen.getByRole("listitem", { name: "Exciter" });
    await userEvent.click(within(exciter).getByRole("button", { name: s.connections.disconnect("Exciter") }));
    const confirm = within(exciter).getByRole("group", { name: s.connections.confirmDisconnect("Exciter") });
    await userEvent.click(within(confirm).getByRole("button", { name: s.connections.confirm }));
    expect(await within(exciter).findByRole("button", { name: s.connections.connect("Exciter") })).toBeInTheDocument();
    const calls = fetch.mock.calls.map(([request]) => `${request.method} ${new URL(request.url).pathname}`);
    expect(calls).toContain(`DELETE /api/v1/tools/${EXCITER.id}/connection`);
  });

  it("pass axe", async () => {
    await renderAt("/connections?error=mismatch", {
      answers: {
        "/api/v1/tools": { tools: [EXCITER, { ...STATOR, status: "needs_registration" }] },
        "/api/v1/connections": { connections: [CONNECTED] },
      },
    });
    await screen.findByRole("listitem", { name: "Exciter" });
    await userEvent.click(screen.getByRole("button", { name: s.connections.disconnect("Exciter") }));
    expect(await axeViolations()).toEqual([]);
  });
});
