import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { ApiError } from "@/api/client";
import { setTraceSetting, type TraceSetting, traceSettingQuery, traceSettingQueryKey } from "@/api/traces";
import { MAX_TRACE_RETENTION_DAYS, MIN_TRACE_RETENTION_DAYS } from "@/config";
import { s } from "@/strings";
import { input, primary } from "./styles";
import { SettingsCard } from "./ui";

/** How long traces are kept and whether Rotor keeps their text, for administrators (#242). */
export function TraceKeepingCard() {
  const setting = useQuery(traceSettingQuery);
  const t = s.traceDestinations.keeping;
  return (
    <SettingsCard titleId="trace-keeping-title" title={t.title} description={t.body} data-trace-keeping>
      {setting.isPending && <p className="text-sm text-ink-muted">{t.loading}</p>}
      {setting.isError && (
        <p role="alert" className="text-sm text-danger">
          {setting.error.message}
        </p>
      )}
      {setting.data && <KeepingForm key={setting.data.updatedAt} current={setting.data} />}
    </SettingsCard>
  );
}

function KeepingForm({ current }: { current: TraceSetting }) {
  const id = useId();
  const client = useQueryClient();
  const t = s.traceDestinations.keeping;
  const [days, setDays] = useState(String(current.retentionDays));
  const [keepContent, setKeepContent] = useState(current.keepContent);
  const save = useMutation({
    mutationFn: () => setTraceSetting({ retentionDays: Number(days), keepContent }),
    onSuccess: (next) => client.setQueryData(traceSettingQueryKey, next),
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate();
  };
  const fieldError = save.error instanceof ApiError ? save.error.fields.retentionDays : undefined;
  return (
    <form className="flex flex-col gap-3" onSubmit={submit} noValidate aria-labelledby="trace-keeping-title">
      <p className="text-sm text-ink" data-trace-keeping-current={current.retentionDays}>
        {t.current(current.retentionDays, current.keepContent)} {current.changedBy && t.changedBy(current.changedBy)}
      </p>
      <div className="flex flex-col gap-1">
        <label htmlFor={`${id}-days`} className="text-sm font-medium text-ink">
          {t.days}
        </label>
        <input
          id={`${id}-days`}
          className={`${input} max-w-32`}
          type="number"
          inputMode="numeric"
          min={MIN_TRACE_RETENTION_DAYS}
          max={MAX_TRACE_RETENTION_DAYS}
          value={days}
          onChange={(e) => setDays(e.target.value)}
          aria-describedby={`${id}-hint`}
          aria-invalid={fieldError ? true : undefined}
          data-trace-days
        />
        <p id={`${id}-hint`} className="text-xs text-ink-muted">
          {fieldError ?? t.daysHint(MIN_TRACE_RETENTION_DAYS, MAX_TRACE_RETENTION_DAYS)}
        </p>
      </div>
      <div className="flex flex-col gap-1">
        <label className="flex items-center gap-2 text-sm text-ink">
          <input
            type="checkbox"
            checked={keepContent}
            onChange={(e) => setKeepContent(e.target.checked)}
            aria-describedby={`${id}-text-hint`}
            data-trace-keep-text
          />
          {t.keepText}
        </label>
        <p id={`${id}-text-hint`} className="text-xs text-ink-muted">
          {t.keepTextHint}
        </p>
      </div>
      {save.isError && (
        <p role="alert" className="text-sm text-danger" data-trace-keeping-error>
          {save.error.message}
        </p>
      )}
      {save.isSuccess && (
        <p role="status" className="text-sm text-ink">
          {t.saved}
        </p>
      )}
      <div>
        <button type="submit" className={primary} disabled={save.isPending} data-action="save-trace-keeping">
          {save.isPending ? t.saving : t.save}
        </button>
      </div>
    </form>
  );
}
