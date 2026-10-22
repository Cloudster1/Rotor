import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { s } from "@/strings";
import { renderAt } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("unattended writes", () => {
  it("are allowed or forbidden by an administrator for everyone", async () => {
    let current = { allowed: true, updatedAt: "2026-10-22T08:00:00Z" };
    const sent: unknown[] = [];
    await renderAt("/settings", {
      answers: {
        "/api/v1/settings/unattended-writes": () => Response.json(current),
        "PUT /api/v1/settings/unattended-writes": async (request: Request) => {
          const body = (await request.json()) as { allowed: boolean };
          sent.push(body);
          current = { ...current, allowed: body.allowed };
          return Response.json(current);
        },
      },
    });
    const allowed = await screen.findByRole("checkbox", { name: s.triggers.unattendedSetting.allowed });
    expect(allowed).toBeChecked();
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(allowed);
    await waitFor(() => expect(screen.getByRole("checkbox", { name: s.triggers.unattendedSetting.allowed })).not.toBeChecked());
    expect(sent).toEqual([{ allowed: false }]);
  });
});
