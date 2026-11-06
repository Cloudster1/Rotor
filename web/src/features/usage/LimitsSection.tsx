import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { ApiError } from "@/api/client";
import { type Limits, setLimits, usageQueryKey } from "@/api/usage";
import { input, primary } from "@/features/settings/styles";
import { SettingsCard } from "@/features/settings/ui";
import { s } from "@/strings";

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
    <SettingsCard titleId={`${id}-title`} title={s.usage.limitsTitle} description={s.usage.limitsBody} data-limits>
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
    </SettingsCard>
  );
}
