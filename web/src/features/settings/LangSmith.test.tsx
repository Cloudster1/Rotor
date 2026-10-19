import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { LANGSMITH_OFF, ME, renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const REFUSED = {
  error: {
    code: "data_leaves_unacknowledged",
    message: "Turning LangSmith on sends runs' prompts, answers and tool calls to LangSmith's servers. Confirm that this is allowed.",
  },
};

describe("the LangSmith setting", () => {
  it("is off until an administrator gives a key and confirms that run data may leave", async () => {
    const sent: Record<string, unknown>[] = [];
    let current = { ...LANGSMITH_OFF };
    await renderAt("/settings", {
      answers: {
        "/api/v1/settings/langsmith": () => Response.json(current),
        "PUT /api/v1/settings/langsmith": async (request: Request) => {
          const body = (await request.json()) as Record<string, unknown>;
          sent.push(body);
          if (body.enabled && !body.acknowledgeDataLeaves) return Response.json(REFUSED, { status: 422 });
          current = { ...current, enabled: true, project: String(body.project), keySet: true, updatedAt: "2026-10-19T09:00:00Z" };
          return Response.json(current);
        },
      },
    });
    const section = await screen.findByRole("region", { name: s.langsmith.title });
    expect(within(section).getByText(s.langsmith.warning)).toBeInTheDocument();
    expect(within(section).getByText(new RegExp(s.langsmith.off))).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(section).getByLabelText(s.langsmith.enabled));
    await userEvent.type(within(section).getByLabelText(s.langsmith.key), "lsv2_pt_example");
    await userEvent.click(within(section).getByRole("button", { name: s.langsmith.save }));
    expect(await within(section).findByRole("alert")).toHaveTextContent("Confirm that this is allowed");

    await userEvent.click(within(section).getByLabelText(s.langsmith.acknowledge));
    await userEvent.click(within(section).getByRole("button", { name: s.langsmith.save }));
    await waitFor(() => expect(within(section).getByText(new RegExp(s.langsmith.on))).toBeInTheDocument());
    expect(sent.at(-1)).toMatchObject({ enabled: true, project: "rotor", key: "lsv2_pt_example", acknowledgeDataLeaves: true });
    expect(within(section).getByLabelText(s.langsmith.key)).toHaveValue("");
  });

  it("is not shown to members", async () => {
    await renderAt("/settings", { me: { ...ME, role: "member" } });
    await screen.findByRole("heading", { level: 1 });
    expect(screen.queryByRole("region", { name: s.langsmith.title })).toBeNull();
  });
});
