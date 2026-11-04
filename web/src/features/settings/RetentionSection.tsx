import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { type Retention, retentionQuery, retentionQueryKey, setRetention } from "@/api/retention";
import { s } from "@/strings";
import { input, primary } from "./styles";
import { SettingsCard } from "./ui";

/** How long a finished run's data is kept, for administrators (#41). */
export function RetentionSection() {
  const setting = useQuery(retentionQuery);
  return (
    <SettingsCard titleId="retention-title" title={s.retention.title} description={s.retention.body} data-retention>
      {setting.isPending && <p className="text-sm text-ink-muted">{s.retention.loading}</p>}
      {setting.isError && (
        <p role="alert" className="text-sm text-danger">
          {setting.error.message}
        </p>
      )}
      {setting.data && <RetentionForm key={setting.data.updatedAt} current={setting.data} />}
    </SettingsCard>
  );
}

function RetentionForm({ current }: { current: Retention }) {
  const id = useId();
  const client = useQueryClient();
  const [days, setDays] = useState(String(current.runDataDays));
  const save = useMutation({
    mutationFn: () => setRetention(Number(days)),
    onSuccess: (next) => client.setQueryData(retentionQueryKey, next),
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate();
  };
  return (
    <form className="flex flex-col gap-3" onSubmit={submit} noValidate aria-labelledby="retention-title">
      <p className="text-sm text-ink" data-retention-current={current.runDataDays}>
        {s.retention.current(current.runDataDays)} {current.changedBy && s.retention.changedBy(current.changedBy)}
      </p>
      <div className="flex flex-col gap-1">
        <label htmlFor={`${id}-days`} className="text-sm font-medium text-ink">
          {s.retention.days}
        </label>
        <input
          id={`${id}-days`}
          className={`${input} max-w-32`}
          type="number"
          inputMode="numeric"
          min={1}
          max={current.maxDays}
          value={days}
          onChange={(e) => setDays(e.target.value)}
          aria-describedby={`${id}-hint`}
          data-retention-days
        />
        <p id={`${id}-hint`} className="text-xs text-ink-muted">
          {s.retention.daysHint(current.maxDays)}
        </p>
      </div>
      {save.isError && (
        <p role="alert" className="text-sm text-danger" data-retention-error>
          {save.error.message}
        </p>
      )}
      {save.isSuccess && (
        <p role="status" className="text-sm text-ink">
          {s.retention.saved}
        </p>
      )}
      <div>
        <button type="submit" className={primary} disabled={save.isPending}>
          {save.isPending ? s.retention.saving : s.retention.save}
        </button>
      </div>
    </form>
  );
}
