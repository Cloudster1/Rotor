import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TraceList, TraceSummary } from "@/api/traces";
import { OTLP_PATH, OWN_TRACES_PAGE_SIZE, SENDING_SPANS_DOC_URL } from "@/config";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";
import { detail, failedTrace } from "@/test/trace";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const MEMBER = { ...ME, role: "member" as const, groups: [] };
const BOB = { id: "0190a5c4-0000-7000-8000-0000000000b0", name: "Bob Builder" };
const LIST = "/api/v1/traces";
const MANY = OWN_TRACES_PAGE_SIZE + 2;

function summary(n: number, overrides: Partial<TraceSummary> = {}): TraceSummary {
  return {
    traceId: n.toString(16).padStart(32, "0"),
    sender: BOB,
    service: "billing-app",
    rootName: `ask ${n}`,
    spans: n + 1,
    failed: false,
    cut: false,
    startedAt: "2026-11-01T10:00:00Z",
    endedAt: "2026-11-01T10:00:02Z",
    ...overrides,
  };
}

/** Answers the list a page at a time from total traces, and keeps the query of each request. */
function pages(total: number) {
  const asked: URLSearchParams[] = [];
  const answer = (request: Request) => {
    const query = new URL(request.url).searchParams;
    asked.push(query);
    const offset = Number(query.get("offset") ?? 0);
    const count = Math.max(0, Math.min(OWN_TRACES_PAGE_SIZE, total - offset));
    const traces = Array.from({ length: count }, (_, i) => summary(offset + i, { failed: offset + i === 0, cut: offset + i === 1 }));
    return Response.json({ traces, total, limit: OWN_TRACES_PAGE_SIZE, offset } satisfies TraceList);
  };
  return { asked, answer };
}

const heads = () => screen.getAllByRole("columnheader").map((cell) => cell.textContent);

describe("the Traces page", () => {
  it("lists a member's own traces with their service, root span, spans, start and failure, and pages through them", async () => {
    const { asked, answer } = pages(MANY);
    await renderAt("/traces", { me: MEMBER, answers: { [LIST]: answer } });
    const table = await screen.findByRole("table", { name: s.traces.label });
    expect(heads()).toEqual([s.traces.columns.trace, s.traces.columns.service, s.traces.columns.spans, s.traces.columns.started, s.traces.columns.status]);
    expect(within(table).getAllByRole("row")).toHaveLength(OWN_TRACES_PAGE_SIZE + 1);
    expect(screen.getByText(s.traces.intro)).toBeInTheDocument();

    const failed = table.querySelector(`[data-trace="${summary(0).traceId}"]`) as HTMLElement;
    expect(within(failed).getByText(s.traces.failed)).toBeInTheDocument();
    expect(within(failed).getByText("billing-app")).toBeInTheDocument();
    expect(within(failed).getByRole("link", { name: "ask 0" })).toHaveAttribute("href", `/traces/${summary(0).traceId}?sender=${BOB.id}`);
    const cut = table.querySelector(`[data-trace="${summary(1).traceId}"]`) as HTMLElement;
    expect(within(cut).getByText(s.traces.ok)).toBeInTheDocument();
    expect(within(cut).getByText(s.traces.cut)).toBeInTheDocument();
    // A member reads only their own, so the list is asked for no sender and shows none.
    expect(asked[0]?.get("sender")).toBeNull();
    expect(table.querySelector("[data-trace-sender]")).toBeNull();
    expect(await axeViolations()).toEqual([]);

    const pager = screen.getByRole("navigation", { name: s.traces.pages });
    await userEvent.click(within(pager).getByRole("button", { name: s.access.pager.nextPage }));
    await waitFor(() => expect(screen.getAllByRole("row")).toHaveLength(MANY - OWN_TRACES_PAGE_SIZE + 1));
    expect(asked.at(-1)?.get("offset")).toBe(String(OWN_TRACES_PAGE_SIZE));
    expect(screen.getByText("ask 51")).toBeInTheDocument();
    expect(within(pager).getByRole("button", { name: s.access.pager.nextPage })).toBeDisabled();
  });

  it("shows an administrator every sender's traces with who sent each, and only theirs on request", async () => {
    const { asked, answer } = pages(3);
    await renderAt("/traces", { answers: { [LIST]: answer } });
    await screen.findByRole("table", { name: s.traces.label });
    expect(heads()).toContain(s.traces.columns.sender);
    expect(screen.getAllByText(BOB.name)).toHaveLength(3);
    expect(screen.getByText(s.traces.introAdmin)).toBeInTheDocument();
    expect(asked[0]?.get("sender")).toBeNull();
    expect(screen.queryByRole("navigation", { name: s.traces.pages })).toBeNull();

    await userEvent.click(screen.getByRole("checkbox", { name: s.traces.mine }));
    await waitFor(() => expect(asked.at(-1)?.get("sender")).toBe(ME.user.id));
    await waitFor(() => expect(heads()).not.toContain(s.traces.columns.sender));
  });

  it("says how to send spans when there is no trace yet", async () => {
    await renderAt("/traces", { me: MEMBER });
    expect(await screen.findByRole("heading", { name: s.traces.emptyTitle })).toBeInTheDocument();
    const help = document.querySelector("[data-send-spans]") as HTMLElement;
    expect(help.textContent).toContain(`OTEL_EXPORTER_OTLP_ENDPOINT=${window.location.origin}${OTLP_PATH}`);
    expect(help.textContent).toContain("OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20<token>");
    const docs = within(help).getByRole("link", { name: s.traces.docs });
    expect(docs).toHaveAttribute("href", SENDING_SPANS_DOC_URL);
    expect(docs).toHaveAttribute("target", "_blank");
    expect(screen.queryByRole("table")).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });
});

describe("an owned trace's page", () => {
  it("shows the trace in the run page's tree and details, the failed span open, its whole span read from the owned route", async () => {
    const { trace, tool } = failedTrace();
    const owned = { ...trace, runId: null, sender: BOB };
    const path = `/api/v1/traces/${trace.traceId}`;
    const spanPath = `${path}/spans/${tool.spanId}`;
    const args = { value: '{"title": ""}', mimeType: "application/json", cut: false };
    const { fetch } = await renderAt(`/traces/${trace.traceId}?sender=${BOB.id}`, {
      answers: { [path]: { trace: owned }, [spanPath]: { span: detail(tool, { input: args }) } },
    });
    expect(await screen.findByRole("heading", { level: 1, name: s.traces.page.title("run") })).toBeInTheDocument();
    const tree = screen.getByRole("tree", { name: s.traces.page.spansTitle });
    expect(within(tree).getAllByRole("treeitem")).toHaveLength(7);
    // No run says it failed, yet the span the trace failed in is chosen, as a failed run's is.
    const details = screen.getByRole("region", { name: s.trace.details.title("mcp create_requirement") });
    await waitFor(() => expect(details.querySelector('[data-span-text="input"]')?.textContent).toBe('{\n  "title": ""\n}'));
    const read = fetch.mock.calls.map(([request]) => new URL((request as Request).url)).find((url) => url.pathname === spanPath);
    expect(read?.searchParams.get("sender")).toBe(BOB.id);
    expect(screen.getByText((_, node) => node?.getAttribute("data-trace-state") === "kept").textContent).toContain(s.trace.spans(7));
    const about = document.querySelector("[data-own-trace-about]") as HTMLElement;
    expect(within(about).getByText(BOB.name)).toBeInTheDocument();
    expect(within(about).getByText(trace.traceId)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(screen.getByRole("link", { name: s.traces.page.back }));
    expect(await screen.findByRole("heading", { level: 1, name: s.places.traces })).toBeInTheDocument();
  });

  it("says when the trace cannot be read", async () => {
    await renderAt("/traces/4bf92f3577b34da6a3ce929d0e0e4736");
    expect(await screen.findByRole("alert")).toHaveTextContent("Not found.");
    expect(screen.queryByRole("tree")).toBeNull();
  });
});
