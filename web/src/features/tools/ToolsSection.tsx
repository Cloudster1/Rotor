import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { meQuery } from "@/api/auth";
import { ApiError } from "@/api/client";
import { addTool, checkTool, lacks, removeTool, type Tool, toolName, toolsQuery, toolsQueryKey } from "@/api/tools";
import { CAPABILITY_WEBHOOKS, CONNECTION_CALLBACK_PATH, HOOK_PRODUCTS } from "@/config";
import { s } from "@/strings";
import { HookPanel } from "./HookPanel";

const card = "max-w-xl rounded-overlay border border-border bg-surface p-5 shadow-1";
const input = "h-9 w-full rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";
const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/** The tools Rotor acts in; an administrator adds, registers again and removes them here. */
export function ToolsSection() {
  const tools = useQuery(toolsQuery);
  const me = useQuery(meQuery);
  const admin = me.data?.role === "admin";
  return (
    <section className={card} aria-labelledby="tools-title" data-tools>
      <h2 id="tools-title" className="mb-1 text-lg font-semibold text-ink">
        {s.tools.title}
      </h2>
      <p className="mb-4 text-sm text-ink-muted">{s.tools.body}</p>
      {tools.isPending && <p className="text-sm text-ink-muted">{s.tools.loading}</p>}
      {tools.isError && (
        <p role="alert" className="text-sm text-danger">
          {tools.error instanceof ApiError ? tools.error.message : s.tools.unreachable}
        </p>
      )}
      {tools.data && tools.data.length === 0 && <p className="text-sm text-ink-muted">{admin ? s.tools.noneAdmin : s.tools.none}</p>}
      {tools.data && tools.data.length > 0 && (
        <ul className="mb-4 flex flex-col gap-3">
          {tools.data.map((tool) => (
            <ToolRow key={tool.id} tool={tool} admin={admin} />
          ))}
        </ul>
      )}
      {admin && <AddTool />}
    </section>
  );
}

function ToolRow({ tool, admin }: { tool: Tool; admin: boolean }) {
  const client = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const remove = useMutation({ mutationFn: () => removeTool(tool.id), onSuccess: () => client.invalidateQueries({ queryKey: toolsQueryKey }) });
  const again = useMutation({ mutationFn: () => addTool({ baseURL: tool.baseURL }), onSuccess: () => client.invalidateQueries({ queryKey: toolsQueryKey }) });
  const check = useMutation({ mutationFn: () => checkTool(tool.id), onSuccess: () => client.invalidateQueries({ queryKey: toolsQueryKey }) });
  const missing = tool.missing ?? [];
  const name = toolName(tool);
  const ready = tool.status === "ready";
  return (
    <li className="rounded-control border border-border p-3" data-tool={tool.name} data-status={tool.status}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="font-medium text-ink">{name}</span>
        <span className={`rounded-control px-2 py-0.5 text-xs ${ready ? "bg-success-subtle text-success" : "bg-warning-subtle text-warning"}`}>
          {s.tools.status[tool.status]}
        </span>
      </div>
      <p className="break-all text-sm text-ink-muted">{tool.baseURL}</p>
      {tool.status === "awaiting_approval" && <p className="mt-1 text-sm text-ink-muted">{s.capabilities.awaiting(name)}</p>}
      <dl className="mt-2 grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-xs">
        <dt className="text-ink-muted">{s.tools.capabilities}</dt>
        <dd className="break-words text-ink">{tool.capabilities.join(", ")}</dd>
        <dt className="text-ink-muted">{s.tools.scopes}</dt>
        <dd className="text-ink">{tool.scopesSupported.join(" ")}</dd>
        <dt className="text-ink-muted">{s.tools.client}</dt>
        <dd className="text-ink">{s.tools.registration[tool.registration]}</dd>
        {missing.length > 0 && (
          <>
            <dt className="text-ink-muted">{s.capabilities.missing}</dt>
            <dd className="text-warning" data-missing>
              {missing.map((m) => (
                <p key={m.capability}>{s.capabilities.lacks(name, m.capability)}</p>
              ))}
            </dd>
          </>
        )}
      </dl>
      {admin && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          {tool.status === "needs_registration" && (
            <button type="button" className={secondary} disabled={again.isPending} onClick={() => again.mutate()} data-action="register-again">
              {s.tools.registerAgain}
            </button>
          )}
          <button type="button" className={secondary} disabled={check.isPending} onClick={() => check.mutate()} data-action="check-tool">
            {check.isPending ? s.capabilities.checking : s.capabilities.checkAgain}
          </button>
          {!confirming ? (
            <button type="button" className={secondary} onClick={() => setConfirming(true)} data-action="remove-tool">
              {s.tools.remove}
            </button>
          ) : (
            <span className="flex flex-wrap items-center gap-2" role="group" aria-label={s.tools.confirmRemove(name)}>
              <span className="text-sm text-ink">{s.tools.confirmRemove(name)}</span>
              <button
                type="button"
                className="h-8 rounded-control bg-danger px-3 text-sm font-medium text-on-danger hover:bg-danger-hover disabled:opacity-60"
                disabled={remove.isPending}
                onClick={() => remove.mutate()}
                data-action="confirm-remove"
              >
                {s.tools.confirm}
              </button>
              <button type="button" className={secondary} onClick={() => setConfirming(false)}>
                {s.tools.keep}
              </button>
            </span>
          )}
        </div>
      )}
      {admin && (HOOK_PRODUCTS as readonly string[]).includes(tool.name) && !lacks(tool, CAPABILITY_WEBHOOKS) && <HookPanel toolID={tool.id} name={name} />}
      {check.isSuccess && (
        <p role="status" className="mt-2 text-sm text-success">
          {s.capabilities.checked(name)}
        </p>
      )}
      {(remove.isError || again.isError || check.isError) && (
        <p role="alert" className="mt-2 text-sm text-danger">
          {message(remove.error ?? again.error ?? check.error)}
        </p>
      )}
    </li>
  );
}

function AddTool() {
  const client = useQueryClient();
  const id = useId();
  const [baseURL, setBaseURL] = useState("");
  const [manual, setManual] = useState(false);
  const [clientID, setClientID] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const add = useMutation({
    mutationFn: () => addTool(manual ? { baseURL, clientID, clientSecret } : { baseURL }),
    onSuccess: () => {
      setBaseURL("");
      setClientID("");
      setClientSecret("");
      return client.invalidateQueries({ queryKey: toolsQueryKey });
    },
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    add.mutate();
  }
  const redirect = typeof window === "undefined" ? CONNECTION_CALLBACK_PATH : `${window.location.origin}${CONNECTION_CALLBACK_PATH}`;
  return (
    <form onSubmit={submit} className="flex flex-col gap-3 border-t border-border pt-4" aria-labelledby={`${id}-title`} data-add-tool>
      <h3 id={`${id}-title`} className="text-base font-semibold text-ink">
        {s.tools.add.title}
      </h3>
      <div className="flex flex-col gap-1">
        <label htmlFor={`${id}-url`} className="text-sm font-medium text-ink">
          {s.tools.add.baseURL}
        </label>
        <input
          id={`${id}-url`}
          name="baseURL"
          type="url"
          required
          className={input}
          value={baseURL}
          onChange={(e) => setBaseURL(e.target.value)}
          aria-describedby={`${id}-url-hint`}
        />
        <p id={`${id}-url-hint`} className="text-xs text-ink-muted">
          {s.tools.add.baseURLHint}
        </p>
      </div>
      <label className="flex items-center gap-2 text-sm text-ink">
        <input type="checkbox" checked={manual} onChange={(e) => setManual(e.target.checked)} name="manual" />
        {s.tools.add.manual}
      </label>
      {manual && (
        <div className="flex flex-col gap-2">
          <p className="break-words text-xs text-ink-muted">{s.tools.add.manualHint(redirect)}</p>
          <label htmlFor={`${id}-client`} className="text-sm font-medium text-ink">
            {s.tools.add.clientID}
          </label>
          <input id={`${id}-client`} name="clientID" required className={input} value={clientID} onChange={(e) => setClientID(e.target.value)} />
          <label htmlFor={`${id}-secret`} className="text-sm font-medium text-ink">
            {s.tools.add.clientSecret}
          </label>
          <input
            id={`${id}-secret`}
            name="clientSecret"
            type="password"
            autoComplete="off"
            required
            className={input}
            value={clientSecret}
            onChange={(e) => setClientSecret(e.target.value)}
          />
        </div>
      )}
      {add.isError && (
        <p role="alert" className="rounded-control border border-danger bg-danger-subtle px-3 py-2 text-sm text-danger" data-add-error>
          {message(add.error)}
        </p>
      )}
      {add.isSuccess && (
        <p role="status" className="text-sm text-success">
          {s.tools.add.added(toolName(add.data))}
        </p>
      )}
      <div>
        <button type="submit" className={primary} disabled={add.isPending} data-action="add-tool">
          {add.isPending ? s.tools.add.adding : s.tools.add.submit}
        </button>
      </div>
    </form>
  );
}
