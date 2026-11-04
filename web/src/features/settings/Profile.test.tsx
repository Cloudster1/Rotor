import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { browserTimeZone, DATE_AND_TIME, setTimeZone } from "@/lib/dates";
import { s } from "@/strings";
import { AXE_TIMEOUT_MS, axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  setTimeZone(null);
});

const TOOL = { id: "0190a5c4-0000-7000-8000-0000000000e1", name: "exciter", baseURL: "https://exciter.test" };
const CONNECTED_AT = "2026-07-01T12:00:00Z";
const CONNECTION = { tool: TOOL, scopes: ["mcp"], status: "connected", connectedAt: CONNECTED_AT, lastUsedAt: null };
const READY = {
  ...TOOL,
  resource: "https://exciter.test/api/v1/mcp",
  mcpURL: "https://exciter.test/api/v1/mcp",
  hookURL: "https://rotor.test/api/v1/hooks/x",
  issuer: "https://exciter.test",
  capabilities: ["mcp@1"],
  missing: [],
  scopesSupported: ["mcp"],
  registration: "dynamic",
  status: "ready",
  addedAt: "2026-10-18T10:00:00Z",
};
const IN = (zone: string) => new Date(CONNECTED_AT).toLocaleString(undefined, { ...DATE_AND_TIME, timeZone: zone });

describe("the profile", () => {
  it("shows the person as the realm gives them, with the sentence on what it is for", async () => {
    await renderAt("/settings/profile");
    expect(await screen.findByRole("heading", { level: 1, name: s.settings.pages.profile })).toBeInTheDocument();
    expect(screen.getByText(s.settings.intros.profile)).toBeInTheDocument();
    const you = screen.getByRole("region", { name: s.account.title });
    expect(within(you).getByText(ME.user.name)).toBeInTheDocument();
    expect(within(you).getByText(ME.user.email)).toBeInTheDocument();
    expect(within(you).getByText("rotor-administrators, engineering")).toBeInTheDocument();
    expect(within(you).getByText(s.account.fromRealm)).toBeInTheDocument();
  });

  it("follows the browser's time zone until one is chosen, and saves the one chosen", async () => {
    let saved: unknown;
    const { fetch } = await renderAt("/settings/profile", {
      answers: {
        "PUT /api/v1/auth/me/preferences": async (request: Request) => {
          saved = await request.json();
          return Response.json(saved);
        },
      },
    });
    const select = await screen.findByRole("combobox", { name: s.timeZone.label });
    expect(select).toHaveValue("");
    expect(within(select).getByRole("option", { name: s.timeZone.browser(browserTimeZone()) })).toBeInTheDocument();

    await userEvent.selectOptions(select, "Pacific/Auckland");
    await userEvent.click(screen.getByRole("button", { name: s.timeZone.save }));
    expect(await screen.findByRole("status")).toHaveTextContent(s.timeZone.saved);
    expect(saved).toEqual({ timeZone: "Pacific/Auckland" });
    expect(fetch.mock.calls.some(([request]) => request.method === "PUT")).toBe(true);
  });

  it("says what to do when the server refuses a zone", async () => {
    await renderAt("/settings/profile", {
      answers: {
        "PUT /api/v1/auth/me/preferences": () =>
          Response.json(
            { error: { code: "validation_failed", message: "Some fields need attention.", fields: { timeZone: "Choose a time zone from the list." } } },
            { status: 422 },
          ),
      },
    });
    await userEvent.selectOptions(await screen.findByRole("combobox", { name: s.timeZone.label }), "UTC");
    await userEvent.click(screen.getByRole("button", { name: s.timeZone.save }));
    expect(await screen.findByRole("alert")).toHaveTextContent("Choose a time zone from the list.");
    expect(screen.getByRole("combobox", { name: s.timeZone.label })).toHaveAttribute("aria-invalid", "true");
  });

  it(
    "passes axe with the time zone chosen",
    async () => {
      await renderAt("/settings/profile", { answers: { "/api/v1/auth/me/preferences": { timeZone: "Europe/Berlin" } } });
      expect(await screen.findByRole("combobox", { name: s.timeZone.label })).toHaveValue("Europe/Berlin");
      expect(await axeViolations()).toEqual([]);
    },
    AXE_TIMEOUT_MS,
  );
});

describe("the time zone chosen", () => {
  it("writes the dates of every page in it from the first draw", async () => {
    await renderAt("/settings/accounts", {
      answers: {
        "/api/v1/auth/me/preferences": { timeZone: "Pacific/Auckland" },
        "/api/v1/tools": { tools: [READY] },
        "/api/v1/connections": { connections: [CONNECTION] },
      },
    });
    expect(await screen.findByText(IN("Pacific/Auckland"))).toBeInTheDocument();
  });

  it("writes them in a zone saved since, on the next page opened", async () => {
    const { router } = await renderAt("/settings/profile", {
      answers: {
        "PUT /api/v1/auth/me/preferences": async (request: Request) => Response.json(await request.json()),
        "/api/v1/tools": { tools: [READY] },
        "/api/v1/connections": { connections: [CONNECTION] },
      },
    });
    await userEvent.selectOptions(await screen.findByRole("combobox", { name: s.timeZone.label }), "America/New_York");
    await userEvent.click(screen.getByRole("button", { name: s.timeZone.save }));
    await screen.findByRole("status");
    await router.navigate({ to: "/settings/accounts" });
    await waitFor(() => expect(screen.getByText(IN("America/New_York"))).toBeInTheDocument());
  });
});
