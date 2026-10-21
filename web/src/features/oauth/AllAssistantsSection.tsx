import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId } from "react";
import { assistantSettingQuery, assistantSettingQueryKey, endGrant, everyonesGrantsQuery, grantsQueryKey, setAssistants } from "@/api/oauth";
import { s } from "@/strings";
import { danger, grantLine } from "./AssistantsSection";

const card = "max-w-xl rounded-overlay border border-border bg-surface p-5 shadow-1";

/** Everybody's connected assistants and whether assistants may connect at all, for administrators (#47). */
export function AllAssistantsSection() {
  const hint = useId();
  const client = useQueryClient();
  const setting = useQuery(assistantSettingQuery);
  const grants = useQuery(everyonesGrantsQuery);
  const toggle = useMutation({
    mutationFn: setAssistants,
    onSuccess: (next) => client.setQueryData(assistantSettingQueryKey, next),
  });
  const end = useMutation({
    mutationFn: (id: string) => endGrant(id, true),
    onSettled: () => client.invalidateQueries({ queryKey: grantsQueryKey }),
  });
  const failed = setting.error ?? grants.error ?? toggle.error ?? end.error;
  return (
    <section className={card} aria-labelledby="all-assistants-title" data-all-assistants>
      <h2 id="all-assistants-title" className="mb-1 text-lg font-semibold text-ink">
        {s.assistants.everyoneTitle}
      </h2>
      <p className="mb-3 text-sm text-ink-muted">{s.assistants.everyoneBody}</p>
      {setting.data && (
        <div className="mb-3 flex flex-col gap-1">
          <label className="flex items-center gap-2 text-sm font-medium text-ink">
            <input
              type="checkbox"
              checked={setting.data.connectionsOn}
              disabled={toggle.isPending}
              onChange={(e) => toggle.mutate(e.target.checked)}
              aria-describedby={hint}
              data-assistants-on
            />
            {s.assistants.switchedOn}
          </label>
          <p id={hint} className="text-xs text-ink-muted">
            {s.assistants.switchHint}
          </p>
        </div>
      )}
      {failed && (
        <p role="alert" className="mb-2 text-sm text-danger">
          {failed.message}
        </p>
      )}
      {grants.data?.length === 0 && <p className="text-sm text-ink-muted">{s.assistants.everyoneNone}</p>}
      {!!grants.data?.length && (
        <ul className="flex flex-col divide-y divide-border">
          {grants.data.map((grant) => {
            const owner = grant.owner.name || grant.owner.email;
            return (
              <li key={grant.id} className="flex items-start justify-between gap-3 py-2" data-owned-grant={grant.client.name}>
                <div className="min-w-0">
                  <p className="text-sm font-medium text-ink">
                    {grant.client.name} <span className="font-normal text-ink-muted">{s.assistants.owner(owner)}</span>
                  </p>
                  <p className="text-xs text-ink-muted">{grantLine(grant)}</p>
                </div>
                <button
                  type="button"
                  className={danger}
                  disabled={end.isPending}
                  aria-label={s.assistants.disconnectFor(grant.client.name, owner)}
                  onClick={() => end.mutate(grant.id)}
                >
                  {s.assistants.disconnect}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
