import { describe, expect, it } from "vitest";
import type { TraceSpan } from "@/api/traces";
import { failedTrace, span } from "@/test/trace";
import { bar, CHILDREN_PAGE, firstFailure, indexSpans, OPEN_ALL_SPANS, readable, type SpanRow, traceWindow, visibleRows } from "./tree";

const NONE = { toggled: new Set<string>(), shown: new Map<string, number>(), path: new Set<string>() };

function names(rows: ReturnType<typeof visibleRows>): string[] {
  return rows.map((r) => (r.kind === "span" ? `${r.depth}:${r.span.name}` : `${r.depth}:more ${r.remaining}`));
}

describe("a trace's tree", () => {
  it("nests every span under its parent, in start order, with its level and place among its siblings", () => {
    const { trace } = failedTrace();
    const rows = visibleRows(trace.roots, { ...NONE, total: trace.spans });
    expect(names(rows)).toEqual([
      "0:run",
      "1:LangGraph",
      "2:node draft",
      "3:ChatOpenAI",
      '3:<img src="x" onerror="alert(1)">',
      "2:node file",
      "3:mcp create_requirement",
    ]);
    const file = rows[5] as SpanRow;
    expect([file.pos, file.size, file.open]).toEqual([2, 2, true]);
    expect(file.parentKey).toBe(trace.roots[0]?.children[0]?.spanId);
  });

  it("closes a span the person closed, and its whole subtree with it", () => {
    const { trace } = failedTrace();
    const draft = trace.roots[0]?.children[0]?.children[0] as TraceSpan;
    const rows = visibleRows(trace.roots, { ...NONE, total: trace.spans, toggled: new Set([draft.spanId]) });
    expect(names(rows)).toEqual(["0:run", "1:LangGraph", "2:node draft", "2:node file", "3:mcp create_requirement"]);
    expect((rows[2] as SpanRow).open).toBe(false);
  });

  it("finds where a failed run failed: the failed span none of whose descendants failed", () => {
    const { trace, tool } = failedTrace();
    const failure = firstFailure(indexSpans(trace.roots));
    expect(failure?.span.spanId).toBe(tool.spanId);
    expect(failure?.ancestors).toHaveLength(3);
    expect(firstFailure(indexSpans([span("run", 0, 10)]))).toBeNull();
  });

  it("opens a large trace only to its nodes and the way to its failure, and pages wide children", () => {
    const wide = Array.from({ length: OPEN_ALL_SPANS }, (_, i) => span(`step ${i}`, i, 1, { children: [span(`inner ${i}`, i, 1)] }));
    const failing = span("failing", 400, 1, { status: "error" });
    const last = span("last step", 500, 1, { children: [failing] });
    const node = span("node loop", 0, 600, { children: [...wide, last] });
    const roots = [span("run", 0, 600, { children: [span("LangGraph", 0, 600, { children: [node] })] })];
    const total = indexSpans(roots).size;
    const plain = visibleRows(roots, { ...NONE, total });
    expect(names(plain)).toEqual(["0:run", "1:LangGraph", "2:node loop"]);

    const loopOpen = { ...NONE, total, toggled: new Set([node.spanId]) };
    const opened = visibleRows(roots, loopOpen);
    expect(opened).toHaveLength(3 + CHILDREN_PAGE + 1);
    expect(names(opened).at(-1)).toBe(`3:more ${OPEN_ALL_SPANS + 1 - CHILDREN_PAGE}`);
    expect(visibleRows(roots, { ...loopOpen, shown: new Map([[node.spanId, OPEN_ALL_SPANS + 1]]) })).toHaveLength(3 + OPEN_ALL_SPANS + 1);

    const failure = firstFailure(indexSpans(roots));
    const path = new Set([...(failure?.ancestors ?? []), failing.spanId]);
    const toFailure = names(visibleRows(roots, { ...NONE, total, path }));
    // The page under the loop reaches the failing step's parent, past CHILDREN_PAGE, and nothing more is left to show.
    expect(toFailure.slice(-2)).toEqual(["3:last step", "4:failing"]);
    expect(toFailure).toHaveLength(3 + OPEN_ALL_SPANS + 1 + 1);
  });

  it("places each span's bar on the trace's time", () => {
    const { trace, model } = failedTrace();
    const w = traceWindow(trace, indexSpans(trace.roots));
    expect(w.length).toBe(2100);
    const place = bar(model, w);
    expect(place.left).toBeCloseTo((200 / 2100) * 100);
    expect(place.width).toBeCloseTo((800 / 2100) * 100);
  });

  it("indents JSON and leaves anything else, a JSON preview cut short too, as it came", () => {
    expect(readable('{"a":[1]}', "application/json")).toBe('{\n  "a": [\n    1\n  ]\n}');
    expect(readable('{"a":', "application/json")).toBe('{"a":');
    expect(readable("<b>x</b>", "text/plain")).toBe("<b>x</b>");
  });
});
