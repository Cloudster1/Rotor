import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useId, useState } from "react";
import { meQuery } from "@/api/auth";
import { removeTrigger, setTriggerEnabled, type Trigger, triggersQueryKey, workflowTriggersQuery } from "@/api/triggers";
import { RunStatusBadge } from "@/features/runs/RunStatusBadge";
import { s } from "@/strings";
import { TriggerForm } from "./TriggerForm";

const card = "rounded-overlay border border-border bg-surface p-4 shadow-1";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";
const danger = "h-8 rounded-control bg-danger px-3 text-sm font-medium text-on-danger hover:bg-danger-hover disabled:opacity-60";

function when(at: string): string {
  return new Date(at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/** The triggers on a workflow that start it as their owner; each person adds their own. */
export function TriggersSection({ workflowID, inputsSchema }: { workflowID: string; inputsSchema: unknown }) {
  const id = useId();
  const list = useQuery(workflowTriggersQuery(workflowID));
  const [editing, setEditing] = useState<Trigger | "new" | null>(null);
  return (
    <section className={card} aria-labelledby={`${id}-title`} data-triggers>
      <div className="mb-1 flex flex-wrap items-baseline justify-between gap-2">
        <h2 id={`${id}-title`} className="text-lg font-semibold text-ink">
          {s.triggers.title}
        </h2>
        {editing === null && (
          <button type="button" className={secondary} onClick={() => setEditing("new")} data-action="add-trigger">
            {s.triggers.add}
          </button>
        )}
      </div>
      <p className="mb-3 text-sm text-ink-muted">{s.triggers.body}</p>
      {editing !== null && (
        <div className="mb-4 rounded-control border border-border p-3">
          <TriggerForm workflowID={workflowID} inputsSchema={inputsSchema} existing={editing === "new" ? undefined : editing} onDone={() => setEditing(null)} />
        </div>
      )}
      {list.data?.length === 0 && editing === null && <p className="text-sm text-ink-muted">{s.triggers.none}</p>}
      {list.isError && (
        <p role="alert" className="text-sm text-danger">
          {list.error.message}
        </p>
      )}
      {list.data && list.data.length > 0 && (
        <ul className="flex flex-col gap-3" aria-label={s.triggers.title}>
          {list.data.map((t) => (
            <TriggerRow key={t.id} trigger={t} onChange={() => setEditing(t)} />
          ))}
        </ul>
      )}
    </section>
  );
}

function TriggerRow({ trigger, onChange }: { trigger: Trigger; onChange: () => void }) {
  const client = useQueryClient();
  const me = useQuery(meQuery);
  const [confirming, setConfirming] = useState(false);
  const mine = me.data?.user.id === trigger.owner.id;
  const refresh = () => client.invalidateQueries({ queryKey: triggersQueryKey });
  const toggle = useMutation({ mutationFn: (on: boolean) => setTriggerEnabled(trigger.id, on), onSuccess: refresh });
  const remove = useMutation({ mutationFn: () => removeTrigger(trigger.id), onSuccess: refresh });
  const fire = trigger.lastFire;
  return (
    <li className="rounded-control border border-border p-3" data-trigger={trigger.name}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="font-medium text-ink">{trigger.name}</span>
        <label className="flex items-center gap-2 text-sm text-ink">
          <input
            type="checkbox"
            checked={trigger.enabled}
            disabled={toggle.isPending || (!mine && !trigger.enabled)}
            onChange={(e) => toggle.mutate(e.target.checked)}
            data-trigger-enabled
          />
          {s.triggers.enabled(trigger.name)}
        </label>
      </div>
      {trigger.schedule && <p className="mt-1 font-mono text-xs text-ink-muted">{s.triggers.schedule(trigger.schedule.cron, trigger.schedule.timeZone)}</p>}
      {!mine && <p className="mt-1 text-xs text-ink-muted">{s.triggers.owner(trigger.owner.name)}</p>}
      <p className="mt-1 text-sm text-ink" data-next-run>
        {trigger.nextRunAt ? s.triggers.next(when(trigger.nextRunAt)) : s.triggers.notScheduled}
      </p>
      {fire && (
        <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-ink" data-last-fire={fire.outcome}>
          {s.triggers.last(s.triggers.outcome[fire.outcome] ?? fire.outcome, when(fire.at))}
          {fire.runId && fire.runStatus && (
            <Link to="/runs/$runID" params={{ runID: fire.runId }} className="hover:underline">
              <RunStatusBadge status={fire.runStatus} />
            </Link>
          )}
        </p>
      )}
      {trigger.disabled && (
        <p className="mt-2 rounded-control border border-warning bg-warning-subtle px-3 py-2 text-sm text-ink" data-trigger-disabled={trigger.disabled.reason}>
          <span className="font-medium">{s.triggers.turnedOff}: </span>
          {trigger.disabled.message}
        </p>
      )}
      <div className="mt-3 flex flex-wrap items-center gap-2">
        {mine && (
          <button type="button" className={secondary} onClick={onChange} data-action="change-trigger">
            {s.triggers.change}
          </button>
        )}
        {!confirming ? (
          <button type="button" className={secondary} onClick={() => setConfirming(true)} data-action="remove-trigger">
            {s.triggers.remove}
          </button>
        ) : (
          <span className="flex flex-wrap items-center gap-2" role="group" aria-label={s.triggers.confirmRemove(trigger.name)}>
            <span className="text-sm text-ink">{s.triggers.confirmRemove(trigger.name)}</span>
            <button type="button" className={danger} disabled={remove.isPending} onClick={() => remove.mutate()} data-action="confirm-remove-trigger">
              {s.triggers.confirm}
            </button>
            <button type="button" className={secondary} onClick={() => setConfirming(false)}>
              {s.triggers.keep}
            </button>
          </span>
        )}
      </div>
      {(toggle.isError || remove.isError) && (
        <p role="alert" className="mt-2 text-sm text-danger" data-trigger-error>
          {(toggle.error ?? remove.error)?.message}
        </p>
      )}
    </li>
  );
}
