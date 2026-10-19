import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { ApiError } from "@/api/client";
import { type Limits, setLimits, usageQueryKey } from "@/api/usage";
import { s } from "@/strings";

const card = "max-w-4xl rounded-overlay border border-border bg-surface p-5 shadow-1";
const input = "h-9 w-full rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";
const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";

type Field = "dailyTokens" | "dailyCost" | "concurrentRuns";
const FIELDS: Field[] = ["dailyTokens", "dailyCost", "concurrentRuns"];

const text = (v: number | null | undefined) => (v === null || v === undefined ? "" : String(v));

/** The limits an administrator sets on everyone: tokens and money a day, and runs going at once; empty is no limit. */
export function LimitsSection({ limits }: { limits: Limits }) {
  const id = useId();
  const client = useQueryClient();
  const [values, setValues] = useState<Record<Field, string>>({
    dailyTokens: text(limits.dailyTokens),
    dailyCost: text(limits.dailyCost),
    concurrentRuns: text(limits.concurrentRuns),
  });
  const save = useMutation({
    mutationFn: () => {
      const read = (f: Field) => (values[f].trim() === "" ? null : Number(values[f]));
      return setLimits({ dailyTokens: read("dailyTokens"), dailyCost: read("dailyCost"), concurrentRuns: read("concurrentRuns") });
    },
    onSuccess: () => client.invalidateQueries({ queryKey: usageQueryKey }),
  });
  const fieldErrors = save.error instanceof ApiError ? save.error.fields : {};
  const submit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate();
  };
  return (
    <section className={card} aria-labelledby={`${id}-title`} data-limits>
      <h2 id={`${id}-title`} className="mb-1 font-semibold text-ink">
        {s.usage.limitsTitle}
      </h2>
      <p className="mb-3 text-sm text-ink-muted">{s.usage.limitsBody}</p>
      <form className="grid gap-3 sm:grid-cols-3" noValidate onSubmit={submit}>
        {FIELDS.map((f) => (
          <div key={f} className="flex flex-col gap-1">
            <label htmlFor={`${id}-${f}`} className="text-sm font-medium text-ink">
              {s.usage[f]}
            </label>
            <input
              id={`${id}-${f}`}
              className={input}
              type="number"
              min={0}
              step={f === "dailyCost" ? "any" : 1}
              value={values[f]}
              onChange={(e) => setValues({ ...values, [f]: e.target.value })}
              aria-invalid={fieldErrors[f] ? true : undefined}
              aria-describedby={fieldErrors[f] ? `${id}-${f}-error` : undefined}
              data-limit={f}
            />
            {fieldErrors[f] && (
              <p id={`${id}-${f}-error`} className="text-sm text-danger">
                {fieldErrors[f]}
              </p>
            )}
          </div>
        ))}
        <div className="sm:col-span-3">
          <button type="submit" className={primary} disabled={save.isPending} data-action="save-limits">
            {s.usage.saveLimits}
          </button>
          {save.isSuccess && (
            <p role="status" className="mt-2 text-sm text-ink">
              {s.usage.savedLimits}
            </p>
          )}
          {save.isError && (
            <p role="alert" className="mt-2 text-sm text-danger">
              {save.error.message}
            </p>
          )}
        </div>
      </form>
    </section>
  );
}
