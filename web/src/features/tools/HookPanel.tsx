import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useEffect, useId, useState } from "react";
import { hookQuery, hookQueryKey, removeHookSecret, setHookSecret } from "@/api/hooks";
import { COPIED_NOTICE_MS } from "@/config";
import { s } from "@/strings";

const input = "h-9 w-full rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";

function when(at: string): string {
  return new Date(at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/** Where a tool delivers its events, and the secret it signs them with, for an administrator to set up (#35). */
export function HookPanel({ toolID, name }: { toolID: string; name: string }) {
  const client = useQueryClient();
  const id = useId();
  const hook = useQuery(hookQuery(toolID));
  const [secret, setSecret] = useState("");
  const [copied, setCopied] = useState<"" | "done" | "failed">("");
  const save = useMutation({
    mutationFn: () => setHookSecret(toolID, secret),
    onSuccess: (data) => {
      setSecret("");
      client.setQueryData(hookQueryKey(toolID), data);
    },
  });
  const forget = useMutation({
    mutationFn: () => removeHookSecret(toolID),
    onSuccess: (data) => client.setQueryData(hookQueryKey(toolID), data),
  });
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(""), COPIED_NOTICE_MS);
    return () => clearTimeout(timer);
  }, [copied]);

  if (hook.isError) {
    return <p className="mt-3 text-xs text-ink-muted">{s.tools.hook.unreachable}</p>;
  }
  if (!hook.data) return null;
  const h = hook.data;
  async function copy() {
    try {
      await navigator.clipboard.writeText(h.url);
      setCopied("done");
    } catch {
      setCopied("failed");
    }
  }
  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate();
  }
  const error = save.error ?? forget.error;
  return (
    <form onSubmit={submit} className="mt-3 flex flex-col gap-2 border-t border-border pt-3" aria-labelledby={`${id}-title`} data-hook>
      <h3 id={`${id}-title`} className="text-sm font-semibold text-ink">
        {s.tools.hook.title}
      </h3>
      <p className="text-xs text-ink-muted">{s.tools.hook.body(name)}</p>
      <label htmlFor={`${id}-url`} className="text-sm font-medium text-ink">
        {s.tools.hook.address}
      </label>
      <div className="flex flex-wrap items-center gap-2">
        <input id={`${id}-url`} readOnly className={`${input} flex-1 font-mono`} value={h.url} data-hook-url />
        <button type="button" className={secondary} onClick={copy} data-action="copy-hook-url">
          {s.tools.hook.copy}
        </button>
      </div>
      {copied && (
        <p role="status" className="text-xs text-ink-muted">
          {copied === "done" ? s.tools.hook.copied : s.tools.hook.copyFailed}
        </p>
      )}
      <p className="text-xs text-ink" data-hook-secret={h.secretSet ? "set" : "missing"}>
        {h.secretSet && h.secretSetAt ? s.tools.hook.set(when(h.secretSetAt)) : s.tools.hook.notSet}{" "}
        {h.lastReceivedAt ? s.tools.hook.lastReceived(when(h.lastReceivedAt)) : s.tools.hook.neverReceived}
      </p>
      <label htmlFor={`${id}-secret`} className="text-sm font-medium text-ink">
        {s.tools.hook.secret}
      </label>
      <input
        id={`${id}-secret`}
        name="hookSecret"
        type="password"
        autoComplete="off"
        required
        className={input}
        value={secret}
        onChange={(e) => setSecret(e.target.value)}
        aria-describedby={`${id}-secret-hint`}
      />
      <p id={`${id}-secret-hint`} className="text-xs text-ink-muted">
        {s.tools.hook.secretHint(name)}
      </p>
      {error && (
        <p role="alert" className="text-sm text-danger">
          {error instanceof Error ? error.message : String(error)}
        </p>
      )}
      {save.isSuccess && (
        <p role="status" className="text-sm text-success">
          {s.tools.hook.saved}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <button type="submit" className={secondary} disabled={save.isPending || secret === ""} data-action="save-hook-secret">
          {s.tools.hook.save}
        </button>
        {h.secretSet && (
          <button type="button" className={secondary} disabled={forget.isPending} onClick={() => forget.mutate()} data-action="remove-hook-secret">
            {s.tools.hook.remove}
          </button>
        )}
      </div>
    </form>
  );
}
