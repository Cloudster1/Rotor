import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { ApiError } from "@/api/client";
import {
  addTraceDestination,
  changeTraceDestination,
  type TraceDestination,
  type TraceDestinationRequest,
  traceDestinationsQueryKey,
} from "@/api/traceDestinations";
import { MAX_DESTINATION_HEADER, MAX_DESTINATION_HEADERS, MAX_DESTINATION_NAME, MAX_DESTINATION_URL, RESERVED_DESTINATION_HEADERS } from "@/config";
import { s } from "@/strings";
import type { DestinationPreset } from "./destinationPresets";
import { input, primary, secondary } from "./styles";

type HeaderRow = { name: string; value: string };
type HeadersMode = "keep" | "replace" | "remove";

/** What a form starts from: a destination being changed, or a preset (or nothing) for a new one. */
export type DestinationStart = { destination: TraceDestination } | { preset?: DestinationPreset };

const t = s.traceDestinations.form;

function Field({ id, label, hint, error, children }: { id: string; label: string; hint?: string; error?: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-sm font-medium text-ink">
        {label}
      </label>
      {children}
      {(error || hint) && (
        <p id={`${id}-hint`} className={`text-xs ${error ? "text-danger" : "text-ink-muted"}`}>
          {error ?? hint}
        </p>
      )}
    </div>
  );
}

/** Adds a destination or changes one; held header values are never shown, so they are kept, replaced or removed. */
export function DestinationForm({ start, onDone }: { start: DestinationStart; onDone: () => void }) {
  const id = useId();
  const client = useQueryClient();
  const existing = "destination" in start ? start.destination : undefined;
  const preset = "preset" in start ? start.preset : undefined;
  const held = existing?.headerNames ?? [];
  const [name, setName] = useState(existing?.name ?? preset?.name ?? "");
  const [endpoint, setEndpoint] = useState(existing?.endpoint ?? preset?.endpoint ?? "");
  const [enabled, setEnabled] = useState(existing?.enabled ?? true);
  const [content, setContent] = useState(existing?.content ?? false);
  // A destination that already holds the admin's confirmation keeps it until its endpoint moves.
  const [acknowledged, setAcknowledged] = useState(existing?.content ?? false);
  const [traceLink, setTraceLink] = useState(existing?.traceLink ?? preset?.traceLink ?? "");
  const [mode, setMode] = useState<HeadersMode>(held.length > 0 ? "keep" : "replace");
  const [rows, setRows] = useState<HeaderRow[]>((preset?.headerNames ?? []).map((headerName) => ({ name: headerName, value: "" })));
  const [missing, setMissing] = useState(false);

  const save = useMutation({
    mutationFn: () => {
      const body: TraceDestinationRequest = {
        name,
        endpoint,
        enabled,
        content,
        acknowledgeContentLeaves: content && acknowledged,
        traceLink: traceLink.trim() || null,
      };
      const filled = rows.filter((row) => row.name.trim() !== "" || row.value !== "");
      if (mode === "remove") body.headers = {};
      else if (mode === "replace" && (filled.length > 0 || held.length > 0))
        body.headers = Object.fromEntries(filled.map((row) => [row.name.trim(), row.value]));
      return existing ? changeTraceDestination(existing.id, body) : addTraceDestination(body);
    },
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: traceDestinationsQueryKey });
      onDone();
    },
  });
  const fields = save.error instanceof ApiError ? save.error.fields : {};
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const lacking = content && !acknowledged;
    setMissing(lacking);
    if (!lacking) save.mutate();
  };
  const moveEndpoint = (next: string) => {
    if (existing?.content && next !== existing.endpoint) setAcknowledged(false);
    setEndpoint(next);
  };
  const setRow = (index: number, change: Partial<HeaderRow>) => setRows(rows.map((row, at) => (at === index ? { ...row, ...change } : row)));
  const field = (suffix: string) => `${id}-${suffix}`;
  const acknowledgeError = missing ? t.acknowledgeMissing : fields.acknowledgeContentLeaves;
  return (
    <form
      className="flex flex-col gap-3 rounded-control border border-border p-3"
      onSubmit={submit}
      noValidate
      aria-label={existing ? s.traceDestinations.editTitle(existing.name) : s.traceDestinations.addTitle}
      data-destination-form
    >
      <Field id={field("name")} label={t.name} hint={t.nameHint} error={fields.name}>
        <input
          id={field("name")}
          className={input}
          value={name}
          maxLength={MAX_DESTINATION_NAME}
          onChange={(e) => setName(e.target.value)}
          aria-describedby={`${field("name")}-hint`}
          aria-invalid={fields.name ? true : undefined}
          data-destination-name
        />
      </Field>
      <Field id={field("endpoint")} label={t.endpoint} hint={t.endpointHint} error={fields.endpoint}>
        <input
          id={field("endpoint")}
          className={input}
          type="url"
          value={endpoint}
          maxLength={MAX_DESTINATION_URL}
          onChange={(e) => moveEndpoint(e.target.value)}
          aria-describedby={`${field("endpoint")}-hint`}
          aria-invalid={fields.endpoint ? true : undefined}
          data-destination-endpoint
        />
      </Field>
      <fieldset className="flex flex-col gap-2" aria-describedby={`${field("headers")}-hint`}>
        <legend className="text-sm font-medium text-ink">{t.headers}</legend>
        <p id={`${field("headers")}-hint`} className={`text-xs ${fields.headers ? "text-danger" : "text-ink-muted"}`}>
          {fields.headers ?? t.headersHint} {t.reserved(RESERVED_DESTINATION_HEADERS.join(", "))}
        </p>
        {held.length > 0 && (
          <div role="radiogroup" aria-label={t.headersMode} className="flex flex-col gap-1 text-sm text-ink">
            {(["keep", "replace", "remove"] as const).map((each) => (
              <label key={each} className="flex items-center gap-2">
                <input type="radio" name={field("mode")} checked={mode === each} onChange={() => setMode(each)} data-headers-mode={each} />
                {each === "keep" ? t.keepHeaders(held) : each === "replace" ? t.replaceHeaders : t.removeHeaders}
              </label>
            ))}
          </div>
        )}
        {mode === "replace" && (
          <>
            {rows.map((row, index) => (
              <div key={index} className="grid grid-cols-[1fr_1fr_auto] items-end gap-2" data-header-row>
                <input
                  className={input}
                  aria-label={`${t.headerName} ${index + 1}`}
                  value={row.name}
                  maxLength={MAX_DESTINATION_HEADER}
                  autoComplete="off"
                  onChange={(e) => setRow(index, { name: e.target.value })}
                  data-header-name
                />
                <input
                  className={input}
                  type="password"
                  aria-label={`${t.headerValue} ${index + 1}`}
                  value={row.value}
                  maxLength={MAX_DESTINATION_HEADER}
                  autoComplete="off"
                  onChange={(e) => setRow(index, { value: e.target.value })}
                  data-header-value
                />
                <button type="button" className={secondary} onClick={() => setRows(rows.filter((_, at) => at !== index))}>
                  {t.removeHeader(index + 1)}
                </button>
              </div>
            ))}
            <div>
              <button
                type="button"
                className={secondary}
                disabled={rows.length >= MAX_DESTINATION_HEADERS}
                onClick={() => setRows([...rows, { name: "", value: "" }])}
                data-action="add-header"
              >
                {t.addHeader}
              </button>
              {rows.length >= MAX_DESTINATION_HEADERS && <span className="ml-2 text-xs text-ink-muted">{t.headersFull(MAX_DESTINATION_HEADERS)}</span>}
            </div>
          </>
        )}
      </fieldset>
      <Field id={field("link")} label={t.link} hint={t.linkHint} error={fields.traceLink}>
        <input
          id={field("link")}
          className={input}
          value={traceLink}
          maxLength={MAX_DESTINATION_URL}
          onChange={(e) => setTraceLink(e.target.value)}
          aria-describedby={`${field("link")}-hint`}
          aria-invalid={fields.traceLink ? true : undefined}
          data-destination-link
        />
      </Field>
      <label className="flex items-center gap-2 text-sm text-ink">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} data-destination-enabled />
        {t.enabled}
      </label>
      <div className="flex flex-col gap-1">
        <label className="flex items-center gap-2 text-sm text-ink">
          <input
            type="checkbox"
            checked={content}
            onChange={(e) => setContent(e.target.checked)}
            aria-describedby={field("content-hint")}
            data-destination-content
          />
          {t.content}
        </label>
        <p id={field("content-hint")} className="text-xs text-ink-muted">
          {t.contentHint}
        </p>
      </div>
      {content && (
        <div className="flex flex-col gap-2">
          <p id={field("warning")} className="rounded-control border border-warning bg-warning-subtle px-3 py-2 text-sm text-ink">
            {t.acknowledgeWarning}
          </p>
          <label className="flex items-start gap-2 text-sm text-ink">
            <input
              type="checkbox"
              className="mt-0.5"
              required
              aria-required
              checked={acknowledged}
              onChange={(e) => setAcknowledged(e.target.checked)}
              aria-describedby={field("warning")}
              aria-invalid={acknowledgeError ? true : undefined}
              data-destination-acknowledge
            />
            {t.acknowledge}
          </label>
          {acknowledgeError && (
            <p role="alert" className="text-sm text-danger">
              {acknowledgeError}
            </p>
          )}
        </div>
      )}
      {save.isError && (
        <p role="alert" className="text-sm text-danger" data-destination-error>
          {save.error.message}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <button type="submit" className={primary} disabled={save.isPending} data-action="save-destination">
          {save.isPending ? t.saving : t.save}
        </button>
        <button type="button" className={secondary} onClick={onDone}>
          {t.cancel}
        </button>
      </div>
    </form>
  );
}
