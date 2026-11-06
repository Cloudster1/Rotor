import { QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Run } from "@/api/runs";
import type { Trace, TraceSpan } from "@/api/traces";
import { createQueryClient } from "@/lib/session";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { stubApi, type Answer } from "@/test/app";
import { detail, failedTrace, PLANTED, RUN, RUN_ID } from "@/test/trace";
import { TraceSection } from "./TraceSection";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const SETTING = { retentionDays: 14, keepContent: true, changedBy: null, updatedAt: "2026-11-01T08:00:00Z" };
const TRACE_PATH = `/api/v1/runs/${RUN_ID}/trace`;
const spanPath = (span: TraceSpan) => `${TRACE_PATH}/spans/${span.spanId}`;

function show(run: Run, trace: Trace, answers: Record<string, Answer> = {}) {
  const fetch = stubApi({ [TRACE_PATH]: { trace }, "/api/v1/settings/traces": SETTING, ...answers });
  const client = createQueryClient(() => undefined);
  render(
    <QueryClientProvider client={client}>
      {/* The run's page around it, so axe judges the section as it stands there. */}
      <main>
        <h1>{run.workflow.name}</h1>
        <TraceSection run={run} />
      </main>
    </QueryClientProvider>,
  );
  return fetch;
}

const SUCCEEDED: Run = { ...RUN, status: "succeeded" };
const item = (name: string) => screen.getByRole("treeitem", { name: new RegExp(`^${name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`) });

describe("a run's trace", () => {
  it("is a tree the keyboard walks, opens and closes, and shows the chosen span whole", async () => {
    const { trace, model } = failedTrace();
    const whole = '{"messages": ["Word the story", "as a user"]}';
    const fetch = show(SUCCEEDED, trace, { [spanPath(model)]: { span: detail(model, { input: { value: whole, mimeType: "application/json", cut: false } }) } });
    const tree = await screen.findByRole("tree", { name: s.trace.title });
    expect(within(tree).getAllByRole("treeitem")).toHaveLength(7);
    expect(item("ChatOpenAI")).toHaveAttribute("aria-level", "4");
    expect(item("node file")).toHaveAttribute("aria-posinset", "2");
    expect(screen.getByText(s.trace.details.empty)).toBeInTheDocument();

    item("run").focus();
    await userEvent.keyboard("{ArrowDown}{ArrowDown}");
    expect(item("node draft")).toHaveFocus();
    await userEvent.keyboard("{ArrowLeft}");
    expect(item("node draft")).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("treeitem", { name: /^ChatOpenAI/ })).toBeNull();
    await userEvent.keyboard("{ArrowRight}{ArrowRight}");
    expect(item("ChatOpenAI")).toHaveFocus();
    await userEvent.keyboard("{Enter}");
    expect(item("ChatOpenAI")).toHaveAttribute("aria-selected", "true");

    const details = screen.getByRole("region", { name: s.trace.details.title("ChatOpenAI") });
    expect(within(details).getByText("stub-model")).toBeInTheDocument();
    expect(within(details).getByText(s.trace.details.tokenCounts(1840, 312, 2152))).toBeInTheDocument();
    expect(within(details).getByText("llm.provider")).toBeInTheDocument();
    // The tree holds the start of the input; the span route answers it whole, indented as JSON.
    await waitFor(() => expect(details.querySelector('[data-span-text="input"]')?.textContent).toBe(JSON.stringify(JSON.parse(whole), null, 2)));
    expect(fetch.mock.calls.some(([request]) => new URL((request as Request).url).pathname === spanPath(model))).toBe(true);

    await userEvent.click(item("LangGraph").querySelector('[data-action="toggle-span"]') as Element);
    expect(screen.queryByRole("treeitem", { name: /^node draft/ })).toBeNull();
    expect(await axeViolations()).toEqual([]);
  });

  it("opens the span a failed run failed in, and marks each failed span in words as well as colour", async () => {
    const { trace, tool } = failedTrace();
    const args = { value: '{"title": ""}', mimeType: "application/json", cut: false };
    show(RUN, trace, { [spanPath(tool)]: { span: detail(tool, { input: args }) } });
    const details = await screen.findByRole("region", { name: s.trace.details.title("mcp create_requirement") });
    await waitFor(() => expect(details.querySelector('[data-span-text="input"]')?.textContent).toBe('{\n  "title": ""\n}'));
    expect(item("mcp create_requirement")).toHaveAttribute("aria-selected", "true");
    expect(within(details).getAllByText(s.trace.details.statuses.error).length).toBeGreaterThan(0);
    expect(within(details).getByText("The tool refused the call.")).toBeInTheDocument();
    expect(within(details).getByText(`${s.trace.details.exception}: ToolError`)).toBeInTheDocument();
    expect(within(details).getByText("Title is required <em>now</em>")).toBeInTheDocument();
    expect(details.querySelector('[data-span-text="exception-stack"]')?.textContent).toContain("Traceback");
    expect(details.querySelector("em")).toBeNull();
    const failed = document.querySelectorAll("[data-span-failed]");
    expect(failed).toHaveLength(4);
    for (const row of failed) expect(row).toHaveTextContent(s.trace.failed);
    expect(await axeViolations()).toEqual([]);
  });

  it("shows span text as text: no markup, no markdown, no link, and says who sent a sender's span", async () => {
    const { trace, model, sent } = failedTrace();
    show(SUCCEEDED, trace, { [spanPath(model)]: { span: detail(model) }, [spanPath(sent)]: { span: detail(sent) } });
    await userEvent.click(await screen.findByText("ChatOpenAI"));
    const details = screen.getByRole("region", { name: s.trace.details.title("ChatOpenAI") });
    await waitFor(() => expect(details.querySelector('[data-span-text="output"]')?.textContent).toBe(PLANTED));
    const section = document.querySelector("[data-run-trace]") as HTMLElement;
    expect(section.querySelector("b, strong, script, img")).toBeNull();
    expect(within(section).queryAllByRole("link")).toHaveLength(0);

    const name = '<img src="x" onerror="alert(1)">';
    expect(item(name)).toHaveTextContent(s.trace.sentBy("Bob Builder"));
    await userEvent.click(within(item(name)).getByText(name));
    const sender = screen.getByRole("region", { name: s.trace.details.title(name) });
    expect(within(sender).getByText(s.trace.details.sender("Bob Builder", "billing-app"))).toBeInTheDocument();
    expect(section.querySelector("img")).toBeNull();
  });

  it("keeps the start of a value when its whole cannot be read", async () => {
    const { trace, model } = failedTrace();
    show(SUCCEEDED, trace, { [spanPath(model)]: () => Response.json({ error: { code: "not_found", message: "There is no such span." } }, { status: 404 }) });
    await userEvent.click(await screen.findByText("ChatOpenAI"));
    const details = screen.getByRole("region", { name: s.trace.details.title("ChatOpenAI") });
    expect(await within(details).findByText(s.trace.details.readFailed("There is no such span."))).toBeInTheDocument();
    expect(details.querySelector('[data-span-text="input"]')?.textContent).toBe('{"messages": ["Word the story"');
  });

  it("says when a trace kept no text, and asks the span route for none", async () => {
    const { trace, model } = failedTrace();
    const bare = (span: TraceSpan): TraceSpan => ({ ...span, input: null, output: null, children: span.children.map(bare) });
    const fetch = show(SUCCEEDED, { ...trace, content: false, roots: trace.roots.map(bare) });
    expect(await screen.findByText(s.trace.noText)).toBeInTheDocument();
    await userEvent.click(screen.getByText("ChatOpenAI"));
    const details = screen.getByRole("region", { name: s.trace.details.title("ChatOpenAI") });
    expect(within(details).getAllByText(s.trace.details.notKept)).toHaveLength(2);
    expect(fetch.mock.calls.some(([request]) => new URL((request as Request).url).pathname === spanPath(model))).toBe(false);
  });

  it("says when a trace expired, was cut or has no span, with the retention, and links each destination", async () => {
    const { trace } = failedTrace();
    show(RUN, { ...trace, state: "expired", roots: [], endedAt: null, expiresAt: "2026-11-15T10:00:00Z" });
    const expired = await screen.findByText((_, node) => node?.getAttribute("data-trace-state") === "expired");
    expect(expired.textContent).toContain("14 days after their last span");
    expect(screen.queryByRole("tree")).toBeNull();
    cleanup();

    const links = [{ destination: "Phoenix", url: "https://phoenix.example/traces/4bf9" }];
    show(SUCCEEDED, { ...trace, cut: true, dropped: 312, links });
    expect(await screen.findByText(s.trace.cut(312))).toBeInTheDocument();
    const link = screen.getByRole("link", { name: s.trace.openIn("Phoenix") });
    expect(link).toHaveAttribute("href", links[0]?.url);
    expect(link).toHaveAttribute("target", "_blank");
    expect(screen.getByText((_, node) => node?.getAttribute("data-trace-state") === "kept").textContent).toContain(s.trace.spans(7));
    cleanup();

    show(SUCCEEDED, { ...trace, state: "none", roots: [], spans: 0, startedAt: null, endedAt: null, expiresAt: null });
    expect(await screen.findByText(s.trace.none)).toBeInTheDocument();
    cleanup();
    show({ ...SUCCEEDED, status: "running", ended: false, finishedAt: null }, { ...trace, state: "none", roots: [], spans: 0 });
    expect(await screen.findByText(s.trace.noneYet)).toBeInTheDocument();
  });
});
