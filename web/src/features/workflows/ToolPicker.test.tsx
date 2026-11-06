import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClientProvider } from "@tanstack/react-query";
import type { Tool } from "@/api/tools";
import { createQueryClient } from "@/lib/session";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { stubApi } from "@/test/app";
import { ToolPicker } from "./ToolPicker";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

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

const CATALOG = `/api/v1/tools/${EXCITER.id}/catalog`;

// The server's sentences for each refusal (#266), as the picker shows them.
const REFUSALS = {
  not_connected: "Connect your account in this tool in Settings, under Tool accounts, first; Rotor lists its tools as your account sees them.",
  reconnect: "This tool ended your connection. Connect your account again in Settings, under Tool accounts, then pick the tool again.",
  needs_registration:
    "This tool no longer knows Rotor. Ask an administrator to register it again under Admin, Tools; then everyone connects their account again.",
};

async function pick(code: keyof typeof REFUSALS) {
  const fetch = stubApi({
    "/api/v1/tools": { tools: [EXCITER] },
    [CATALOG]: () => Response.json({ error: { code, message: REFUSALS[code] } }, { status: 409 }),
    [`POST /api/v1/tools/${EXCITER.id}/connection`]: () => Response.json({ authorizeURL: "https://exciter.test/authorize" }),
  });
  render(
    <QueryClientProvider client={createQueryClient(() => undefined)}>
      <main>
        <h1>Workflow</h1>
        <ToolPicker onInsert={() => undefined} />
      </main>
    </QueryClientProvider>,
  );
  await userEvent.selectOptions(await screen.findByLabelText(s.workflows.tools.tool), EXCITER.id);
  return { fetch, alert: await screen.findByRole("alert") };
}

describe("the tool picker", () => {
  it("says a tool that must be registered again is an administrator's to fix, and offers no connecting", async () => {
    const { alert, fetch } = await pick("needs_registration");
    expect(alert).toHaveTextContent(REFUSALS.needs_registration);
    expect(alert).toHaveAttribute("data-picker-error", "needs_registration");
    expect(screen.queryByRole("button", { name: /Connect/ })).toBeNull();
    // Tool accounts and Admin, Tools are read again, so they say what the picker found.
    await waitFor(() => expect(fetch.mock.calls.filter(([r]) => new URL((r as Request).url).pathname === "/api/v1/tools").length).toBeGreaterThan(1));
    expect(await axeViolations()).toEqual([]);
  });

  it("offers connecting again when the tool ended the connection", async () => {
    const assign = vi.fn();
    vi.stubGlobal("location", { ...window.location, assign, pathname: "/workflows/w1" });
    const { alert } = await pick("reconnect");
    expect(alert).toHaveTextContent(REFUSALS.reconnect);
    await userEvent.click(screen.getByRole("button", { name: s.connections.connectAgain("Exciter") }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith("https://exciter.test/authorize"));
  });

  it("offers connecting when the person never connected", async () => {
    const { alert } = await pick("not_connected");
    expect(alert).toHaveTextContent(REFUSALS.not_connected);
    expect(screen.getByRole("button", { name: s.connections.connect("Exciter") })).toBeInTheDocument();
  });
});
