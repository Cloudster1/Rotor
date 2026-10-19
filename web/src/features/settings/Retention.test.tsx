import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, RETENTION_DEFAULT, renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const OUT_OF_RANGE = {
  error: {
    code: "validation_failed",
    message: "Some fields need attention.",
    fields: { runDataDays: "Keep run data for a whole number of days from 1 to 3650." },
  },
};

describe("the retention setting", () => {
  it("shows how long runs are kept and lets an administrator change it", async () => {
    const sent: unknown[] = [];
    let current: { runDataDays: number; maxDays: number; changedBy: string | null; updatedAt: string } = { ...RETENTION_DEFAULT };
    await renderAt("/settings", {
      answers: {
        "/api/v1/settings/retention": () => Response.json(current),
        "PUT /api/v1/settings/retention": async (request: Request) => {
          const body = (await request.json()) as { runDataDays: number };
          sent.push(body);
          if (body.runDataDays > current.maxDays) return Response.json(OUT_OF_RANGE, { status: 422 });
          current = { ...current, runDataDays: body.runDataDays, changedBy: "Ada Admin", updatedAt: "2026-10-19T09:00:00Z" };
          return Response.json(current);
        },
      },
    });
    const section = await screen.findByRole("region", { name: s.retention.title });
    expect(within(section).getByText(new RegExp(s.retention.current(90)))).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    const days = within(section).getByLabelText(s.retention.days);
    await userEvent.clear(days);
    await userEvent.type(days, "5000");
    await userEvent.click(within(section).getByRole("button", { name: s.retention.save }));
    expect(await within(section).findByRole("alert")).toBeInTheDocument();

    await userEvent.clear(within(section).getByLabelText(s.retention.days));
    await userEvent.type(within(section).getByLabelText(s.retention.days), "30");
    await userEvent.click(within(section).getByRole("button", { name: s.retention.save }));
    await waitFor(() => expect(within(section).getByText(new RegExp(s.retention.current(30)))).toBeInTheDocument());
    expect(within(section).getByText(new RegExp(s.retention.changedBy("Ada Admin")))).toBeInTheDocument();
    expect(sent.at(-1)).toEqual({ runDataDays: 30 });
  });

  it("is not shown to members", async () => {
    await renderAt("/settings", { me: { ...ME, role: "member" } });
    await screen.findByRole("heading", { level: 1 });
    expect(screen.queryByRole("region", { name: s.retention.title })).toBeNull();
  });
});
