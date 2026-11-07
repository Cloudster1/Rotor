import type { Run } from "@/api/runs";
import type { Trace, TraceSpan, TraceSpanDetail } from "@/api/traces";

// A run's trace as #244 answers it, for the tests of the tree and the details: a model call that worked, then a node
// whose tool call failed, and a span a sender added.

export const RUN_ID = "0190a5c4-0000-7000-8000-00000000ee01";
export const BASE = "2026-11-01T10:00:00.000Z";
const BASE_MS = Date.parse(BASE);
/** Span ids are 16 hex characters, as W3C trace context writes them. */
const SPAN_ID_CHARS = 16;
const HEX = 16;

let next = 0;
/** A span starting at ms into the trace and lasting duration ms, with nothing kept but what overrides say. */
export function span(name: string, ms: number, duration: number, overrides: Partial<TraceSpan> = {}): TraceSpan {
  next += 1;
  return {
    spanId: next.toString(HEX).padStart(SPAN_ID_CHARS, "0"),
    parentSpanId: "",
    name,
    kind: "CHAIN",
    service: "rotor-runner",
    source: "runner",
    sender: null,
    startedAt: new Date(BASE_MS + ms).toISOString(),
    endedAt: new Date(BASE_MS + ms + duration).toISOString(),
    durationMs: duration,
    status: "unset",
    statusMessage: "",
    model: "",
    tokens: null,
    attributes: {},
    input: null,
    output: null,
    events: [],
    links: [],
    omitted: 0,
    children: [],
    ...overrides,
  };
}

export const PLANTED = "**bold** <b>loud</b> <script>alert(1)</script> see https://evil.example/x";

export function failedTrace(): { trace: Trace; model: TraceSpan; tool: TraceSpan; sent: TraceSpan } {
  const model = span("ChatOpenAI", 200, 800, {
    kind: "LLM",
    model: "stub-model",
    tokens: { prompt: 1840, completion: 312, total: 2152 },
    input: { value: '{"messages": ["Word the story"', mimeType: "application/json", cut: true },
    output: { value: PLANTED, mimeType: "text/plain", cut: false },
    attributes: { "llm.provider": "openai", "openinference.span.kind": "LLM" },
  });
  const sent = span('<img src="x" onerror="alert(1)">', 1100, 100, {
    source: "otlp",
    service: "billing-app",
    sender: { id: "0190a5c4-0000-7000-8000-0000000000b0", name: "Bob Builder" },
  });
  const draft = span("node draft", 100, 1200, { kind: "AGENT", attributes: { "rotor.node": "draft" }, children: [model, sent] });
  const tool = span("mcp create_requirement", 1500, 300, {
    kind: "TOOL",
    status: "error",
    statusMessage: "The tool refused the call.",
    events: [
      {
        name: "exception",
        at: new Date(BASE_MS + 1800).toISOString(),
        attributes: {
          "exception.type": "ToolError",
          "exception.message": "Title is required <em>now</em>",
          "exception.stacktrace": 'Traceback (most recent call last):\n  File "node.py", line 3, in run',
        },
      },
    ],
  });
  const file = span("node file", 1400, 600, { status: "error", children: [tool] });
  const graph = span("LangGraph", 50, 2000, { status: "error", children: [draft, file] });
  const root = span("run", 0, 2100, { status: "error", children: [graph] });
  return {
    trace: {
      traceId: "4bf92f3577b34da6a3ce929d0e0e4736",
      runId: RUN_ID,
      sender: null,
      state: "kept",
      content: true,
      cut: false,
      dropped: 0,
      spans: 7,
      bytes: 4096,
      startedAt: BASE,
      endedAt: new Date(BASE_MS + 2100).toISOString(),
      expiresAt: "2026-11-15T10:00:00Z",
      links: [],
      roots: [root],
    },
    model,
    tool,
    sent,
  };
}

/** A span as the span route answers it: whole, without children, with its text attributes. */
export function detail(of: TraceSpan, overrides: Partial<TraceSpanDetail> = {}): TraceSpanDetail {
  const { children: _children, omitted: _omitted, ...rest } = of;
  return { ...rest, content: {}, ...overrides };
}

export const RUN: Run = {
  id: RUN_ID,
  workflow: { id: "0190a5c4-0000-7000-8000-00000000bb01", name: "file-story" },
  version: 1,
  status: "failed",
  startedBy: { id: "0190a5c4-0000-7000-8000-000000000001", name: "Ada Admin" },
  reason: "",
  inputs: {},
  output: null,
  createdAt: BASE,
  updatedAt: BASE,
  startedAt: BASE,
  finishedAt: "2026-11-01T10:00:03Z",
  ended: true,
  approvalId: null,
  approvalIds: [],
  traceId: "4bf92f3577b34da6a3ce929d0e0e4736",
  trigger: null,
  usage: { inputTokens: 0, outputTokens: 0, cost: 0 },
};
