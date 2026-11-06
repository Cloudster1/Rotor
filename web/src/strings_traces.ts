import { spanSearchStrings } from "./strings_span_search";

/** The words of the Traces page (#257), the traces senders posted of no run, spread into s as s.traces; Search spans (#261) is its search. */
export const ownTraceStrings = {
  intro: "Traces your own apps sent Rotor that belong to no run, newest first. Open one to read its spans as a run's trace reads.",
  introAdmin:
    "Traces apps sent Rotor that belong to no run, newest first: every sender's, as you administer Rotor. Open one to read its spans as a run's trace reads.",
  label: "Traces",
  loading: "Reading the traces...",
  unreachable: "The traces could not be read. Reload the page to try again.",
  count: { one: "trace", many: "traces" },
  pages: "Pages of traces",
  mine: "Only mine",
  columns: { trace: "Trace", sender: "Sender", service: "Service", spans: "Spans", started: "Started", status: "Status" },
  failed: "Failed",
  ok: "No failure",
  cut: "Cut short",
  unnamed: "No root span",
  emptyTitle: "No traces yet",
  emptyBody:
    "An app that traces with OpenTelemetry, such as a LangChain app, sends its spans to Rotor with two settings. Spans of a trace no run of yours has form a trace of your own, listed here.",
  noneOfMineTitle: "No traces of yours",
  noneOfMine: "You have sent no trace of your own yet; other senders' traces show without Only mine.",
  settings: "Set these two variables where the app runs:",
  token:
    "The token is an access token of an assistant you connected that may act, or an access token of the realm for your account, such as a service account's.",
  docs: "How to send spans (opens in a new tab)",
  search: spanSearchStrings,
  page: {
    back: "All traces",
    title: (name: string) => (name ? `Trace ${name}` : "Trace"),
    spansTitle: "Spans",
    intro: "Each span the app sent, nested as it ran. Choose a span to see its details.",
    id: "Trace id",
    sender: "Sender",
    service: "Service",
    started: "Started",
    none: "This trace holds no span: each one it was sent was refused.",
  },
} as const;
