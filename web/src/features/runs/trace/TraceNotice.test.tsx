import { cleanup, screen } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { s } from "@/strings";
import { ME, renderAt } from "@/test/app";
import { failedTrace, RUN, RUN_ID } from "@/test/trace";

beforeAll(async () => {
  await import("../RunRoute");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const SETTING = { retentionDays: 14, keepContent: false, changedBy: null, updatedAt: "2026-11-01T08:00:00Z" };

async function open(me: typeof ME) {
  const { trace } = failedTrace();
  await renderAt(`/runs/${RUN_ID}`, {
    me,
    answers: {
      [`/api/v1/runs/${RUN_ID}`]: { ...RUN, status: "succeeded" },
      [`/api/v1/runs/${RUN_ID}/trace`]: { trace: { ...trace, content: false } },
      "/api/v1/settings/traces": SETTING,
    },
  });
  return screen.findByText((_, node) => node?.hasAttribute("data-trace-no-text") ?? false);
}

describe("the notice that a trace kept no text", () => {
  it("points an administrator to the page where it is chosen", async () => {
    await open(ME);
    expect(await screen.findByRole("link", { name: s.trace.chooseText })).toHaveAttribute("href", "/admin/traces");
  });

  it("leaves everyone else with the sentence alone", async () => {
    const notice = await open({ ...ME, role: "member" });
    expect(notice).toHaveTextContent(s.trace.noText);
    expect(screen.queryByRole("link", { name: s.trace.chooseText })).toBeNull();
  });
});
