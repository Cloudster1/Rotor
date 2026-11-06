import { cleanup, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { SpanLink } from "@/api/traces";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { renderAt } from "@/test/app";
import { failedTrace } from "@/test/trace";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const BOB = { id: "0190a5c4-0000-7000-8000-0000000000b0", name: "Bob Builder" };
const LINKED_RUN = "0190a5c4-0000-7000-8000-00000000ee02";
const OWN_TRACE = "0af7651916cd43dd8448eb211c80319c";
const HIDDEN_TRACE = "4bf92f3577b34da6a3ce929d0e0e4736";

const LINKS: SpanLink[] = [
  { traceId: "5b8efff798038103d269b633813fc60c", spanId: "eee19b7ec3c1b174", attributes: { "llm.system": "openai" }, runId: LINKED_RUN, sender: null },
  { traceId: OWN_TRACE, spanId: "1111111111111111", attributes: {}, runId: null, sender: BOB.id },
  { traceId: HIDDEN_TRACE, spanId: "2222222222222222", attributes: {}, runId: null, sender: null },
];

describe("a span's links (#262)", () => {
  it("open the run or the own trace the reader may read, and show any other by its ids alone", async () => {
    const { trace, tool } = failedTrace();
    tool.links = LINKS;
    const path = `/api/v1/traces/${trace.traceId}`;
    await renderAt(`/traces/${trace.traceId}?sender=${BOB.id}`, { answers: { [path]: { trace: { ...trace, runId: null, sender: BOB } } } });
    const details = await screen.findByRole("region", { name: s.trace.details.title("mcp create_requirement") });
    const t = s.trace.details.links;
    expect(within(details).getByRole("heading", { name: t.title })).toBeInTheDocument();

    const items = details.querySelectorAll("[data-span-link]");
    expect(items).toHaveLength(LINKS.length);
    expect(within(details).getByRole("link", { name: t.inRun })).toHaveAttribute("href", `/runs/${LINKED_RUN}`);
    expect(within(details).getByRole("link", { name: t.inTrace })).toHaveAttribute("href", `/traces/${OWN_TRACE}?sender=${BOB.id}`);
    expect(within(details).getAllByRole("link", { name: /linked span/ })).toHaveLength(2);

    // A trace the reader may not read says so whether or not it exists, and leads nowhere.
    const hidden = items[2] as HTMLElement;
    expect(within(hidden).queryByRole("link")).toBeNull();
    expect(hidden).toHaveTextContent(t.unreadable);
    expect(hidden).toHaveTextContent(t.ids(HIDDEN_TRACE, "2222222222222222"));
    expect(within(items[0] as HTMLElement).getByText("llm.system")).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("leave the details without a Links heading when a span names none", async () => {
    const { trace } = failedTrace();
    const path = `/api/v1/traces/${trace.traceId}`;
    await renderAt(`/traces/${trace.traceId}?sender=${BOB.id}`, { answers: { [path]: { trace: { ...trace, runId: null, sender: BOB } } } });
    const details = await screen.findByRole("region", { name: s.trace.details.title("mcp create_requirement") });
    expect(within(details).queryByRole("heading", { name: s.trace.details.links.title })).toBeNull();
  });
});
