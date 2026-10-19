import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { AuditEntry } from "@/api/audit";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const ENTRY = (n: number, action = "run.started"): AuditEntry => ({
  id: `0190a5c4-0000-7000-8000-0000000000${String(n).padStart(2, "0")}`,
  action,
  targetType: "run",
  targetId: `run-${n}`,
  actor: { id: ME.user.id, name: ME.user.name },
  data: { workflow: "wf-1", name: "weekly" },
  ip: "192.0.2.1",
  requestId: "",
  at: "2026-10-19T08:00:00Z",
});

const FACETS = { actions: ["run.started", "sandbox.spawned"], actors: [{ id: ME.user.id, name: ME.user.name }], targetTypes: ["run"] };

describe("the audit log", () => {
  it("is filtered, paged by cursor and exported as CSV or JSON", async () => {
    const asked: string[] = [];
    await renderAt("/settings/audit", {
      answers: {
        "/api/v1/audit/facets": FACETS,
        "/api/v1/audit": (request: Request) => {
          const url = new URL(request.url);
          asked.push(url.search);
          const cursor = url.searchParams.get("cursor");
          return Response.json(cursor ? { entries: [ENTRY(3)], next: null } : { entries: [ENTRY(1), ENTRY(2, "sandbox.spawned")], next: "c1" });
        },
      },
    });
    const table = await screen.findByRole("table");
    expect(within(table).getByText("Sandbox started")).toBeInTheDocument();
    expect(within(table).getAllByText(/workflow: wf-1/)[0]).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("button", { name: s.audit.older }));
    expect(await screen.findByText(s.audit.page(2))).toBeInTheDocument();
    await waitFor(() => expect(asked.some((q) => q.includes("cursor=c1"))).toBe(true));

    await userEvent.selectOptions(screen.getByLabelText(s.audit.action), "run.started");
    await waitFor(() => expect(asked.at(-1)).toContain("action=run.started"));
    expect(screen.getByRole("link", { name: s.audit.exportCSV })).toHaveAttribute("href", "/api/v1/audit/export?format=csv&action=run.started");
    expect(screen.getByRole("link", { name: s.audit.exportJSON })).toHaveAttribute("href", "/api/v1/audit/export?format=json&action=run.started");
  });

  it("tells a member it is not theirs to read", async () => {
    await renderAt("/settings/audit", { me: { ...ME, role: "member" } });
    expect(await screen.findByRole("alert")).toHaveTextContent(s.audit.notAllowed);
  });

  it("is linked from the settings for administrators", async () => {
    await renderAt("/settings");
    expect(await screen.findByRole("link", { name: s.audit.open })).toHaveAttribute("href", "/settings/audit");
  });
});
