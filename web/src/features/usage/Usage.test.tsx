import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Provider } from "@/api/providers";
import type { Usage } from "@/api/usage";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const USAGE: Usage = {
  today: { day: "2026-10-19", inputTokens: 1200, outputTokens: 300, cost: 0.42 },
  month: { day: "2026-10-01", inputTokens: 5200, outputTokens: 900, cost: 1.5 },
  days: [
    { day: "2026-10-19", inputTokens: 1200, outputTokens: 300, cost: 0.42 },
    { day: "2026-10-18", inputTokens: 4000, outputTokens: 600, cost: 1.08 },
  ],
  limits: { dailyTokens: 100000, dailyCost: null, concurrentRuns: 2 },
};

const STUB: Provider = {
  id: "0190a5c4-0000-7000-8000-00000000a002",
  name: "Stub",
  kind: "openai_compatible",
  baseURL: "http://stub",
  apiVersion: "",
  models: ["stub-model"],
  defaultModel: "stub-model",
  isDefault: true,
  enabled: true,
  hasKey: true,
};

describe("usage", () => {
  it("shows a member what they used and the limits on them, without the administrators' parts", async () => {
    await renderAt("/settings/usage", { me: { ...ME, role: "member", groups: [] }, answers: { "/api/v1/usage": USAGE } });
    const today = await screen.findByRole("region", { name: s.usage.today });
    expect(within(today).getByText("1200")).toBeInTheDocument();
    expect(screen.getByRole("region", { name: s.usage.yourLimits })).toHaveTextContent(s.usage.noLimit);
    expect(document.querySelectorAll("[data-usage-days] tbody tr")).toHaveLength(2);
    expect(screen.queryByRole("region", { name: s.usage.limitsTitle })).not.toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("lets an administrator set the limits on everyone and see everyone's usage", async () => {
    let sent: Record<string, unknown> | undefined;
    await renderAt("/settings/usage", {
      answers: {
        "/api/v1/usage": USAGE,
        "/api/v1/usage/people": { people: [{ person: { id: ME.user.id, name: ME.user.name }, today: USAGE.today, month: USAGE.month }] },
        "PUT /api/v1/settings/limits": async (request: Request) => {
          sent = (await request.json()) as Record<string, unknown>;
          return Response.json(sent);
        },
      },
    });
    const limits = await screen.findByRole("region", { name: s.usage.limitsTitle });
    await userEvent.clear(within(limits).getByLabelText(s.usage.dailyTokens));
    await userEvent.type(within(limits).getByLabelText(s.usage.dailyCost), "25");
    await userEvent.click(within(limits).getByRole("button", { name: s.usage.saveLimits }));
    await waitFor(() => expect(sent).toEqual({ dailyTokens: null, dailyCost: 25, concurrentRuns: 2 }));
    expect(await screen.findByRole("region", { name: s.usage.peopleTitle })).toHaveTextContent(ME.user.name);
    expect(await axeViolations()).toEqual([]);
  });

  it("lets an administrator price a provider's models", async () => {
    let sent: unknown;
    await renderAt("/settings", {
      answers: {
        "/api/v1/providers": { providers: [STUB] },
        [`/api/v1/providers/${STUB.id}/prices`]: { prices: [] },
        [`PUT /api/v1/providers/${STUB.id}/prices`]: async (request: Request) => {
          sent = await request.json();
          return Response.json(sent);
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: s.usage.prices }));
    await userEvent.type(await screen.findByLabelText(s.usage.inputPrice("stub-model")), "3");
    await userEvent.type(screen.getByLabelText(s.usage.outputPrice("stub-model")), "15");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: s.usage.savePrices }));
    await waitFor(() => expect(sent).toEqual({ prices: [{ model: "stub-model", inputPerMillion: 3, outputPerMillion: 15 }] }));
  });
});
