import { QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { SpanHit, SpanSearchResponse } from "@/api/spans";
import { HTTP_UNPROCESSABLE, SPAN_SEARCH_PAGE_SIZE } from "@/config";
import { TraceSection } from "@/features/runs/trace/TraceSection";
import { createQueryClient } from "@/lib/session";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt, stubApi } from "@/test/app";
import { failedTrace, RUN, RUN_ID } from "@/test/trace";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const SEARCH = "/api/v1/spans";
const WORKFLOW = { id: "0190a5c4-0000-7000-8000-00000000bb01", name: "file-story" };
const BOB = { id: "0190a5c4-0000-7000-8000-0000000000b0", name: "Bob Builder" };
const DAY_MS = 86_400_000;
const t = s.traces.search;

function hit(n: number, overrides: Partial<SpanHit> = {}): SpanHit {
  return {
    spanId: n.toString(16).padStart(16, "0"),
    name: `mcp whoami ${n}`,
    kind: "TOOL",
    service: "rotor-runner",
    source: "runner",
    status: "ok",
    statusMessage: "",
    startedAt: "2026-11-01T10:00:00Z",
    durationMs: 250,
    model: "",
    traceId: "4bf92f3577b34da6a3ce929d0e0e4736",
    runId: RUN_ID,
    workflow: WORKFLOW,
    owner: null,
    ...overrides,
  };
}

/** Answers each search with hits from first on, a page at a time, and keeps what each asked. */
function answers(pages: SpanHit[][]) {
  const asked: URLSearchParams[] = [];
  const answer = (request: Request) => {
    const query = new URL(request.url).searchParams;
    asked.push(query);
    const at = Number(query.get("cursor") ?? 0);
    const next = at + 1 < pages.length ? String(at + 1) : null;
    return Response.json({ spans: pages[at] ?? [], next, since: "", until: "", limit: SPAN_SEARCH_PAGE_SIZE } satisfies SpanSearchResponse);
  };
  return { asked, answer };
}

describe("Search spans", () => {
  it("finds spans of runs and own traces, each opening its trace with the span chosen, and pages on", async () => {
    const failed = hit(1, { status: "error", statusMessage: "The tool refused the call." });
    const own = hit(2, { runId: null, workflow: null, owner: BOB, source: "otlp", service: "billing-app", name: "lookup" });
    const { asked, answer } = answers([[failed, own], [hit(3)]]);
    const before = Date.now();
    await renderAt("/traces/search", { me: { ...ME, role: "member" as const, groups: [] }, answers: { [SEARCH]: answer } });

    const table = await screen.findByRole("table", { name: t.label });
    const first = table.querySelector(`[data-span-hit="${failed.spanId}"]`) as HTMLElement;
    expect(within(first).getByRole("link", { name: failed.name })).toHaveAttribute("href", `/runs/${RUN_ID}?span=${failed.spanId}`);
    expect(within(first).getByText(s.trace.failed)).toBeInTheDocument();
    expect(within(first).getByText(failed.statusMessage)).toBeInTheDocument();
    expect(within(first).getByText(t.runOf(WORKFLOW.name))).toBeInTheDocument();
    const second = table.querySelector(`[data-span-hit="${own.spanId}"]`) as HTMLElement;
    expect(within(second).getByRole("link", { name: "lookup" })).toHaveAttribute("href", `/traces/${own.traceId}?sender=${BOB.id}&span=${own.spanId}`);
    expect(within(second).getByText(t.ownTrace(BOB.name))).toBeInTheDocument();
    expect(screen.getByText(t.shown(2))).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    // A search naming nothing reads the last week, and no status.
    const since = Date.parse(asked[0]?.get("since") ?? "");
    expect(since).toBeGreaterThanOrEqual(before - 7 * DAY_MS);
    expect(since).toBeLessThan(Date.now() - 6 * DAY_MS);
    expect(asked[0]?.get("status")).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: t.more }));
    await screen.findByRole("link", { name: "mcp whoami 3" });
    expect(asked.at(-1)?.get("cursor")).toBe("1");
    // The next page reads the window the first did, though time went on.
    expect(asked.at(-1)?.get("since")).toBe(asked[0]?.get("since"));
    expect(screen.queryByRole("button", { name: t.more })).toBeNull();
  });

  it("asks for what the form names, failed spans as status error, and keeps it in the address", async () => {
    const { asked, answer } = answers([[]]);
    const { router } = await renderAt("/traces/search", { answers: { [SEARCH]: answer, "/api/v1/workflows": { workflows: [WORKFLOW] } } });
    expect(await screen.findByText(t.none)).toBeInTheDocument();
    const form = screen.getByRole("form", { name: t.form });
    await userEvent.type(within(form).getByLabelText(t.name), "whoami");
    await userEvent.selectOptions(within(form).getByLabelText(t.kind), "TOOL");
    await userEvent.selectOptions(await within(form).findByLabelText(t.workflow), WORKFLOW.id);
    await userEvent.selectOptions(within(form).getByLabelText(t.window), "30");
    await userEvent.type(within(form).getByLabelText(t.attrKey), "rotor.node");
    await userEvent.type(within(form).getByLabelText(t.attrValue), "ask");
    await userEvent.click(within(form).getByLabelText(t.failed));
    await userEvent.click(within(form).getByRole("button", { name: t.submit }));

    await waitFor(() => expect(asked).toHaveLength(2));
    const q = asked[1] as URLSearchParams;
    expect(Object.fromEntries(q)).toMatchObject({
      name: "whoami",
      kind: "TOOL",
      status: "error",
      workflow: WORKFLOW.id,
      attrKey: "rotor.node",
      attrValue: "ask",
    });
    expect(Date.parse(q.get("since") ?? "")).toBeLessThan(Date.now() - 29 * DAY_MS);
    expect(router.state.location.search).toMatchObject({ name: "whoami", kind: "TOOL", failed: true, days: 30 });
  });

  it("says why the API refused a search", async () => {
    const refusal = { error: { code: "validation_failed", message: "Some fields need attention.", fields: { attrKey: "That attribute carries text." } } };
    await renderAt("/traces/search?attrKey=input.value&attrValue=x", {
      answers: { [SEARCH]: () => Response.json(refusal, { status: HTTP_UNPROCESSABLE }) },
    });
    expect(await screen.findByRole("alert")).toHaveTextContent(t.failedSearch("That attribute carries text."));
  });
});

describe("a span a search opens", () => {
  it("is chosen in the run's trace before the span the run failed in", async () => {
    const { trace, model } = failedTrace();
    stubApi({
      [`/api/v1/runs/${RUN_ID}/trace`]: { trace },
      "/api/v1/settings/traces": { retentionDays: 14, keepContent: true, changedBy: null, updatedAt: "" },
    });
    render(
      <QueryClientProvider client={createQueryClient(() => undefined)}>
        <TraceSection run={RUN} focus={model.spanId} />
      </QueryClientProvider>,
    );
    const chosen = await screen.findByRole("treeitem", { name: /^ChatOpenAI/ });
    expect(chosen).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("treeitem", { name: /^mcp create_requirement/ })).toHaveAttribute("aria-selected", "false");
  });
});
