const MS_PER_SECOND = 1000;
const SECONDS_PER_MINUTE = 60;

/** A span's length as people say it: milliseconds under a second, seconds with a tenth under a minute, else minutes. */
function duration(ms: number): string {
  if (ms < MS_PER_SECOND) return `${Math.round(ms)} ms`;
  const seconds = ms / MS_PER_SECOND;
  if (seconds < SECONDS_PER_MINUTE) return `${seconds.toFixed(1)} s`;
  const minutes = Math.floor(seconds / SECONDS_PER_MINUTE);
  return `${minutes} min ${Math.round(seconds - minutes * SECONDS_PER_MINUTE)} s`;
}

/** The words of a run's trace on its page (#245), spread into s as s.trace; strings.ts holds the rest. */
export const traceStrings = {
  title: "Trace",
  intro: "Each step, model call and tool call of the run, nested as they ran. Choose a span to see its details.",
  loading: "Reading the trace...",
  unreadable: (message: string) => `The trace could not be read: ${message}`,
  spans: (n: number) => (n === 1 ? "1 span" : `${n} spans`),
  keptUntil: (date: string, days: number | null) =>
    days === null ? `Kept until ${date}.` : `Kept until ${date}, ${days} ${days === 1 ? "day" : "days"} after its last span.`,
  none: "This run has no trace.",
  noneYet: "No span has arrived yet; the trace grows as the run goes.",
  expired: (date: string | null, days: number | null) =>
    `This trace expired${date ? ` on ${date}` : ""}: Rotor keeps traces ${days === null ? "for a while" : `${days} ${days === 1 ? "day" : "days"}`} after their last span. Its counts remain.`,
  cut: (dropped: number) =>
    `This trace reached its limit: ${dropped === 1 ? "1 more span was" : `${dropped} more spans were`} not kept. The spans below are the ones that came first.`,
  noText:
    "Text is not kept: inputs, outputs and messages are left out of traces, and only timing, status, attributes and errors are kept. An administrator chooses whether Rotor keeps text.",
  chooseText: "Choose in Admin, Traces",
  links: "Open this trace in",
  openIn: (destination: string) => `${destination} (opens in a new tab)`,
  tree: "Spans",
  timeline: "Spans and their time",
  more: (shown: number, remaining: number) => `Show more (${shown} shown, ${remaining} more)`,
  omitted: (n: number) => (n === 1 ? "1 span left out" : `${n} spans left out`),
  failed: "Failed",
  sentBy: (name: string) => `sent by ${name}`,
  duration,
  kinds: {
    CHAIN: "Chain",
    LLM: "Model",
    TOOL: "Tool",
    AGENT: "Agent",
    RETRIEVER: "Retriever",
    EMBEDDING: "Embedding",
    RERANKER: "Reranker",
    GUARDRAIL: "Guardrail",
    EVALUATOR: "Evaluator",
    PROMPT: "Prompt",
    UNKNOWN: "Other",
  },
  details: {
    title: (name: string) => `Span ${name}`,
    empty: "Choose a span in the tree to see its details.",
    kind: "Kind",
    status: "Status",
    statuses: { ok: "Succeeded", unset: "Finished", error: "Failed" },
    message: "Status message",
    started: "Started",
    ended: "Ended",
    took: "Took",
    after: (offset: string) => `${offset} after the trace began`,
    sentBy: "Sent by",
    runner: "Rotor's runner",
    sender: (name: string, service: string) => (service ? `${name}, as ${service}` : name),
    model: "Model",
    tokens: "Tokens",
    tokenCounts: (prompt: number, completion: number, total: number) => `${prompt} in, ${completion} out, ${total} in all`,
    attributes: "Attributes",
    noAttributes: "No attributes.",
    text: "Text attributes",
    events: "Events",
    exception: "Exception",
    stack: "Stack trace",
    input: "Input",
    output: "Output",
    reading: "Reading the whole value...",
    readFailed: (message: string) => `Only the start is shown; the whole value could not be read: ${message}`,
    preview: "Only the start is shown.",
    cut: "Cut short when it was kept.",
    notKept: "Not kept: Rotor keeps no text of traces.",
    nothing: "None.",
    links: {
      title: "Links",
      inRun: "Open the linked span's run",
      inTrace: "Open the linked span's trace",
      unreadable: "You may not read the linked trace, or Rotor does not hold it.",
      ids: (traceID: string, spanID: string) => `Trace ${traceID}, span ${spanID}`,
    },
  },
} as const;
