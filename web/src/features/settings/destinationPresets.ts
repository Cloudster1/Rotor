import { s } from "@/strings";

/** What a preset fills into a new destination's form; the administrator edits it before saving. */
export interface DestinationPreset {
  key: "langfuse" | "phoenix" | "jaeger" | "langsmith";
  name: string;
  endpoint: string;
  headerNames: string[];
  traceLink: string;
  hint: string;
}

// The endpoints are the tools' documented OTLP/HTTP trace addresses; the in-cluster hosts are the usual compose names.
export const DESTINATION_PRESETS: readonly DestinationPreset[] = [
  {
    key: "langfuse",
    name: s.traceDestinations.presets.langfuse,
    endpoint: "https://cloud.langfuse.com/api/public/otel/v1/traces",
    headerNames: ["Authorization"],
    traceLink: "https://cloud.langfuse.com/project/<project id>/traces/{traceId}",
    hint: s.traceDestinations.presetHints.langfuse,
  },
  {
    key: "phoenix",
    name: s.traceDestinations.presets.phoenix,
    endpoint: "http://phoenix:6006/v1/traces",
    headerNames: [],
    traceLink: "",
    hint: s.traceDestinations.presetHints.phoenix,
  },
  {
    key: "jaeger",
    name: s.traceDestinations.presets.jaeger,
    endpoint: "http://jaeger:4318/v1/traces",
    headerNames: [],
    traceLink: "http://localhost:16686/trace/{traceId}",
    hint: s.traceDestinations.presetHints.jaeger,
  },
  {
    key: "langsmith",
    name: s.traceDestinations.presets.langsmith,
    endpoint: "https://api.smith.langchain.com/otel/v1/traces",
    headerNames: ["x-api-key", "Langsmith-Project"],
    traceLink: "",
    hint: s.traceDestinations.presetHints.langsmith,
  },
];
