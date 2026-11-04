import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { ApiError } from "@/api/client";
import { type Preferences, preferencesQuery, preferencesQueryKey, setPreferences } from "@/api/preferences";
import { browserTimeZone, DATE_AND_TIME, formatDateTime, setTimeZone, timeZones } from "@/lib/dates";
import { s } from "@/strings";
import { input, primary } from "./styles";
import { SettingsCard } from "./ui";

// The select's value for following the browser, which no IANA name can be.
const BROWSER = "";

/** The zone a person's dates are written in, the browser's until they choose one (#206). */
export function TimeZoneCard() {
  const saved = useQuery(preferencesQuery);
  return (
    <SettingsCard titleId="time-zone-title" title={s.timeZone.title} description={s.timeZone.body} data-time-zone>
      {saved.isPending && <p className="text-sm text-ink-muted">{s.timeZone.loading}</p>}
      {saved.isError && (
        <p role="alert" className="text-sm text-danger">
          {saved.error.message}
        </p>
      )}
      {saved.data && <TimeZoneForm saved={saved.data} />}
    </SettingsCard>
  );
}

function TimeZoneForm({ saved }: { saved: Preferences }) {
  const id = useId();
  const client = useQueryClient();
  const [zone, setZone] = useState(saved.timeZone ?? BROWSER);
  const save = useMutation({
    mutationFn: () => setPreferences({ timeZone: zone || null }),
    onSuccess: (answer: Preferences) => {
      setTimeZone(answer.timeZone);
      client.setQueryData(preferencesQueryKey, answer);
    },
  });
  const zones = timeZones();
  // A zone chosen in another browser that this one does not know is still offered, so saving keeps it.
  if (zone && !zones.includes(zone)) zones.unshift(zone);
  const problem = save.error instanceof ApiError ? (save.error.fields.timeZone ?? save.error.message) : save.error?.message;
  const preview = formatDateTime(Date.now(), { ...DATE_AND_TIME, timeZone: zone || browserTimeZone() });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate();
  };
  return (
    <form className="flex flex-col gap-3" onSubmit={submit}>
      <div className="flex flex-col gap-1">
        <label htmlFor={`${id}-zone`} className="text-sm font-medium text-ink">
          {s.timeZone.label}
        </label>
        <select
          id={`${id}-zone`}
          className={input}
          value={zone}
          onChange={(event) => {
            setZone(event.target.value);
            save.reset();
          }}
          aria-invalid={problem ? true : undefined}
          aria-describedby={`${id}-preview`}
          data-time-zone-select
        >
          <option value={BROWSER}>{s.timeZone.browser(browserTimeZone())}</option>
          {zones.map((each) => (
            <option key={each} value={each}>
              {each.replaceAll("_", " ")}
            </option>
          ))}
        </select>
        <p id={`${id}-preview`} className="text-xs text-ink-muted" data-time-zone-preview>
          {s.timeZone.preview(preview)}
        </p>
      </div>
      {problem && (
        <p role="alert" className="text-sm text-danger">
          {problem}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <button type="submit" className={primary} disabled={save.isPending} data-action="save-time-zone">
          {save.isPending ? s.timeZone.saving : s.timeZone.save}
        </button>
        {save.isSuccess && (
          <p role="status" className="text-sm text-success">
            {s.timeZone.saved}
          </p>
        )}
      </div>
    </form>
  );
}
