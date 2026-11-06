import { OTLP_PATH, SENDING_SPANS_DOC_URL } from "@/config";
import { link } from "@/features/settings/styles";
import { s } from "@/strings";

/** How an app sends Rotor its spans (#243): the two variables its stock exporter reads, and the docs. */
export function SendSpans() {
  const t = s.traces;
  // The SDK adds /v1/traces to the endpoint, and the specification writes the header's space percent-encoded.
  const settings = [`OTEL_EXPORTER_OTLP_ENDPOINT=${window.location.origin}${OTLP_PATH}`, "OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20<token>"];
  return (
    <div className="flex w-full flex-col gap-2 text-left text-sm text-ink" data-send-spans>
      <p>{t.settings}</p>
      <pre className="rounded-control border border-border bg-surface-raised p-3 font-mono text-xs break-all whitespace-pre-wrap">{settings.join("\n")}</pre>
      <p className="text-ink-muted">{t.token}</p>
      <a href={SENDING_SPANS_DOC_URL} target="_blank" rel="noreferrer" className={link} data-send-spans-docs>
        {t.docs}
      </a>
    </div>
  );
}
