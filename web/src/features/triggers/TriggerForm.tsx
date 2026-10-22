import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { ApiError } from "@/api/client";
import { changeTrigger, createTrigger, previewSchedule, type Trigger, triggersQueryKey } from "@/api/triggers";
import { HTTP_UNPROCESSABLE } from "@/config";
import { fieldsOf, InputsForm, inputsOf, type Values, valuesOf } from "@/features/runs/InputsForm";
import { s } from "@/strings";
import { draftOf, EventFields, eventOf } from "./EventFields";

const input = "h-9 w-full rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";
const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";
const secondary = "h-9 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";

/** The zones a browser knows, UTC first; an older browser offers UTC alone. */
function zones(): string[] {
  const known = typeof Intl.supportedValuesOf === "function" ? Intl.supportedValuesOf("timeZone") : [];
  return ["UTC", ...known.filter((z) => z !== "UTC")];
}

function localZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}

/** Adds a schedule to a workflow or changes one: its name, cron fields, time zone, overlap and the inputs it starts with. */
export function TriggerForm({
  workflowID,
  inputsSchema,
  unattendedCalls = [],
  existing,
  onDone,
}: {
  workflowID: string;
  inputsSchema: unknown;
  /** The <binding>.<call> pairs the workflow lets write with no approval, which the owner must agree to (#128). */
  unattendedCalls?: string[];
  existing?: Trigger;
  onDone: () => void;
}) {
  const id = useId();
  const client = useQueryClient();
  const [kind, setKind] = useState<"schedule" | "event">(existing?.kind ?? "schedule");
  const [draft, setDraft] = useState(draftOf(existing?.event));
  // An event trigger's runs get the event as their input event, so the form never asks for it.
  const fields = kind === "event" ? (fieldsOf(inputsSchema)?.filter((f) => f.name !== "event") ?? null) : fieldsOf(inputsSchema);
  const [name, setName] = useState(existing?.name ?? "");
  const [cron, setCron] = useState(existing?.schedule?.cron ?? "0 8 * * 1");
  const [zone, setZone] = useState(existing?.schedule?.timeZone ?? localZone());
  const [noOverlap, setNoOverlap] = useState(existing?.schedule?.noOverlap ?? true);
  const [values, setValues] = useState<Values>(valuesOf(fields, existing?.inputs as Record<string, unknown> | undefined));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [nextRuns, setNextRuns] = useState<string[] | null>(null);
  const [agreed, setAgreed] = useState(unattendedCalls.every((call) => existing?.allowUnattended.includes(call)));

  const fieldErrors = (err: unknown) => {
    if (!(err instanceof ApiError) || err.status !== HTTP_UNPROCESSABLE) return {};
    const out: Record<string, string> = {};
    for (const [key, message] of Object.entries(err.fields)) {
      const inputKey = key.startsWith("inputs/") ? key.slice("inputs/".length) : key;
      out[inputKey] = message;
    }
    return out;
  };
  const preview = useMutation({
    mutationFn: () => previewSchedule(cron, zone),
    onSuccess: (runs) => {
      setNextRuns(runs);
      setErrors({});
    },
    onError: (err) => {
      setNextRuns(null);
      setErrors(fieldErrors(err));
    },
  });
  const save = useMutation({
    mutationFn: (inputs: Record<string, unknown>) => {
      const allowUnattended = agreed ? unattendedCalls : [];
      const body =
        kind === "event"
          ? { name, kind: "event" as const, event: eventOf(draft), inputs, allowUnattended }
          : { name, kind: "schedule" as const, schedule: { cron, timeZone: zone, noOverlap }, inputs, allowUnattended };
      return existing ? changeTrigger(existing.id, body) : createTrigger(workflowID, body);
    },
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: triggersQueryKey });
      onDone();
    },
    onError: (err) => setErrors(fieldErrors(err)),
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const read = inputsOf(fields, values);
    setErrors(read.errors);
    if (Object.keys(read.errors).length === 0) save.mutate(read.inputs);
  };
  const field = (suffix: string) => `${id}-${suffix}`;
  const described = (key: string, hint?: string) => [hint, errors[key] ? field(`${key}-error`) : undefined].filter(Boolean).join(" ") || undefined;
  const error = (key: string, at: string) =>
    errors[key] ? (
      <p id={field(`${at}-error`)} className="text-sm text-danger">
        {errors[key]}
      </p>
    ) : null;

  return (
    <form className="flex flex-col gap-3" noValidate onSubmit={submit} aria-labelledby={field("title")} data-trigger-form>
      <h3 id={field("title")} className="text-base font-semibold text-ink">
        {existing ? s.triggers.change : s.triggers.add}
      </h3>
      <div className="flex flex-col gap-1">
        <label htmlFor={field("name")} className="text-sm font-medium text-ink">
          {s.triggers.name}
        </label>
        <input
          id={field("name")}
          className={input}
          value={name}
          onChange={(e) => setName(e.target.value)}
          aria-invalid={errors.name ? true : undefined}
          aria-describedby={described("name")}
          data-trigger-name
        />
        {error("name", "name")}
      </div>
      {!existing && (
        <fieldset className="flex flex-wrap gap-4">
          <legend className="mb-1 text-sm font-medium text-ink">{s.triggers.kind}</legend>
          {(["schedule", "event"] as const).map((k) => (
            <label key={k} className="flex items-center gap-2 text-sm text-ink">
              <input type="radio" name={field("kind")} checked={kind === k} onChange={() => setKind(k)} data-trigger-kind={k} />
              {s.triggers.kinds[k]}
            </label>
          ))}
        </fieldset>
      )}
      {kind === "event" && <EventFields draft={draft} onChange={setDraft} errors={errors} />}
      {kind === "schedule" && (
        <>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1">
              <label htmlFor={field("cron")} className="text-sm font-medium text-ink">
                {s.triggers.cron}
              </label>
              <input
                id={field("cron")}
                className={`${input} font-mono`}
                value={cron}
                onChange={(e) => setCron(e.target.value)}
                aria-invalid={errors["schedule/cron"] ? true : undefined}
                aria-describedby={described("schedule/cron", field("cron-hint"))}
                data-trigger-cron
              />
              <p id={field("cron-hint")} className="text-xs text-ink-muted">
                {s.triggers.cronHint}
              </p>
              {error("schedule/cron", "schedule/cron")}
            </div>
            <div className="flex flex-col gap-1">
              <label htmlFor={field("zone")} className="text-sm font-medium text-ink">
                {s.triggers.timeZone}
              </label>
              <select
                id={field("zone")}
                className={input}
                value={zone}
                onChange={(e) => setZone(e.target.value)}
                aria-invalid={errors["schedule/timeZone"] ? true : undefined}
                aria-describedby={described("schedule/timeZone")}
                data-trigger-zone
              >
                {zones().map((z) => (
                  <option key={z} value={z}>
                    {z}
                  </option>
                ))}
              </select>
              {error("schedule/timeZone", "schedule/timeZone")}
            </div>
          </div>
          <label className="flex items-center gap-2 text-sm text-ink">
            <input type="checkbox" checked={noOverlap} onChange={(e) => setNoOverlap(e.target.checked)} data-trigger-no-overlap />
            {s.triggers.noOverlap}
          </label>
        </>
      )}
      {(fields === null || fields.length > 0) && (
        <fieldset className="flex flex-col gap-2">
          <legend className="mb-1 text-sm font-medium text-ink">{s.triggers.inputs}</legend>
          <InputsForm fields={fields} values={values} errors={errors} onChange={(key, value) => setValues({ ...values, [key]: value })} />
        </fieldset>
      )}
      {kind === "schedule" && (
        <div>
          <button type="button" className={secondary} disabled={preview.isPending} onClick={() => preview.mutate()} data-action="preview-schedule">
            {s.triggers.preview}
          </button>
          {nextRuns && (
            <div className="mt-2">
              <p className="text-sm font-medium text-ink">{s.triggers.nextRuns}</p>
              <ul className="text-sm text-ink" data-next-runs>
                {nextRuns.map((at) => (
                  <li key={at}>{new Date(at).toLocaleString(undefined, { dateStyle: "full", timeStyle: "short", timeZone: zone })}</li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}
      {unattendedCalls.length > 0 && (
        <fieldset className="flex flex-col gap-1" data-trigger-unattended>
          <legend className="mb-1 text-sm font-medium text-ink">{s.triggers.unattended.title}</legend>
          <p id={field("unattended-hint")} className="text-xs text-ink-muted">
            {s.triggers.unattended.body}
          </p>
          <ul className="list-inside list-disc font-mono text-xs text-ink">
            {unattendedCalls.map((call) => (
              <li key={call}>{call}</li>
            ))}
          </ul>
          <label className="flex items-center gap-2 text-sm text-ink">
            <input
              type="checkbox"
              checked={agreed}
              onChange={(e) => setAgreed(e.target.checked)}
              aria-invalid={errors.allowUnattended ? true : undefined}
              aria-describedby={described("allowUnattended", field("unattended-hint"))}
              data-trigger-agree-unattended
            />
            {s.triggers.unattended.agree}
          </label>
          {error("allowUnattended", "allowUnattended")}
        </fieldset>
      )}
      {save.isError && (
        <p role="alert" className="rounded-control border border-danger bg-danger-subtle px-3 py-2 text-sm text-danger" data-trigger-error>
          {save.error.message}
        </p>
      )}
      <div className="flex gap-2">
        <button type="submit" className={primary} disabled={save.isPending} data-action="save-trigger">
          {s.triggers.save}
        </button>
        <button type="button" className={secondary} onClick={onDone}>
          {s.triggers.cancel}
        </button>
      </div>
    </form>
  );
}
