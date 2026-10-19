import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { type LangSmith, langSmithQuery, langSmithQueryKey, removeLangSmithKey, setLangSmith } from "@/api/langsmith";
import { MAX_LANGSMITH_KEY, MAX_LANGSMITH_PROJECT } from "@/config";
import { s } from "@/strings";

const card = "max-w-xl rounded-overlay border border-border bg-surface p-5 shadow-1";
const input = "h-9 w-full rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";
const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";
const danger = "h-8 rounded-control bg-danger px-3 text-sm font-medium text-on-danger hover:bg-danger-hover disabled:opacity-60";

/** Whether runs' traces also go to LangSmith, for administrators: off until one confirms that run data may leave. */
export function LangSmithSection() {
  const setting = useQuery(langSmithQuery);
  return (
    <section className={card} aria-labelledby="langsmith-title" data-langsmith>
      <h2 id="langsmith-title" className="mb-1 text-lg font-semibold text-ink">
        {s.langsmith.title}
      </h2>
      <p className="mb-3 text-sm text-ink-muted">{s.langsmith.body}</p>
      {setting.isPending && <p className="text-sm text-ink-muted">{s.langsmith.loading}</p>}
      {setting.isError && (
        <p role="alert" className="text-sm text-danger">
          {setting.error.message}
        </p>
      )}
      {setting.data && <LangSmithForm key={setting.data.updatedAt} current={setting.data} />}
    </section>
  );
}

function LangSmithForm({ current }: { current: LangSmith }) {
  const id = useId();
  const client = useQueryClient();
  const [enabled, setEnabled] = useState(current.enabled);
  const [project, setProject] = useState(current.project);
  const [key, setKey] = useState("");
  const [acknowledged, setAcknowledged] = useState(current.enabled);
  const [confirming, setConfirming] = useState(false);
  const refresh = (next: LangSmith) => client.setQueryData(langSmithQueryKey, next);
  const save = useMutation({
    mutationFn: () => setLangSmith({ enabled, project, key: key || undefined, acknowledgeDataLeaves: acknowledged }),
    onSuccess: refresh,
  });
  const forget = useMutation({ mutationFn: removeLangSmithKey, onSuccess: refresh });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate();
  };
  const field = (suffix: string) => `${id}-${suffix}`;
  return (
    <form className="flex flex-col gap-3" onSubmit={submit} noValidate aria-labelledby="langsmith-title">
      <p className="text-sm text-ink" data-langsmith-state={current.enabled ? "on" : "off"}>
        {current.enabled ? s.langsmith.on : s.langsmith.off} {s.langsmith.endpoint(current.endpoint)}
      </p>
      <p id={field("warning")} className="rounded-control border border-warning bg-warning-subtle px-3 py-2 text-sm text-ink">
        {s.langsmith.warning}
      </p>
      <label className="flex items-center gap-2 text-sm text-ink">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} data-langsmith-enabled />
        {s.langsmith.enabled}
      </label>
      <div className="flex flex-col gap-1">
        <label htmlFor={field("project")} className="text-sm font-medium text-ink">
          {s.langsmith.project}
        </label>
        <input
          id={field("project")}
          className={input}
          value={project}
          maxLength={MAX_LANGSMITH_PROJECT}
          onChange={(e) => setProject(e.target.value)}
          aria-describedby={field("project-hint")}
        />
        <p id={field("project-hint")} className="text-xs text-ink-muted">
          {s.langsmith.projectHint}
        </p>
      </div>
      <div className="flex flex-col gap-1">
        <label htmlFor={field("key")} className="text-sm font-medium text-ink">
          {s.langsmith.key}
        </label>
        <input
          id={field("key")}
          className={input}
          type="password"
          autoComplete="off"
          value={key}
          maxLength={MAX_LANGSMITH_KEY}
          onChange={(e) => setKey(e.target.value)}
          aria-describedby={field("key-hint")}
          data-langsmith-key
        />
        <p id={field("key-hint")} className="text-xs text-ink-muted">
          {s.langsmith.keyHint} {current.keySet ? s.langsmith.keyKept : s.langsmith.noKey}
        </p>
      </div>
      <label className="flex items-start gap-2 text-sm text-ink">
        <input
          type="checkbox"
          className="mt-0.5"
          checked={acknowledged}
          onChange={(e) => setAcknowledged(e.target.checked)}
          aria-describedby={field("warning")}
          data-langsmith-acknowledge
        />
        {s.langsmith.acknowledge}
      </label>
      {save.isError && (
        <p role="alert" className="text-sm text-danger" data-langsmith-error>
          {save.error.message}
        </p>
      )}
      {save.isSuccess && (
        <p role="status" className="text-sm text-ink">
          {s.langsmith.saved}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <button type="submit" className={primary} disabled={save.isPending} data-action="save-langsmith">
          {save.isPending ? s.langsmith.saving : s.langsmith.save}
        </button>
        {current.keySet &&
          (!confirming ? (
            <button type="button" className={secondary} onClick={() => setConfirming(true)} data-action="remove-langsmith-key">
              {s.langsmith.removeKey}
            </button>
          ) : (
            <span className="flex flex-wrap items-center gap-2" role="group" aria-label={s.langsmith.confirmRemoveKey}>
              <span className="text-sm text-ink">{s.langsmith.confirmRemoveKey}</span>
              <button type="button" className={danger} disabled={forget.isPending} onClick={() => forget.mutate()} data-action="confirm-remove-langsmith-key">
                {s.langsmith.confirm}
              </button>
              <button type="button" className={secondary} onClick={() => setConfirming(false)}>
                {s.langsmith.keep}
              </button>
            </span>
          ))}
      </div>
    </form>
  );
}
