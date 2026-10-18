import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import {
  addProvider,
  changeProvider,
  type Provider,
  type ProviderRequest,
  providersQuery,
  providersQueryKey,
  removeProvider,
  testProvider,
} from "@/api/providers";
import { s } from "@/strings";

const card = "max-w-xl rounded-overlay border border-border bg-surface p-5 shadow-1";
const input = "h-9 w-full rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";
const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";
const KINDS = ["anthropic", "openai", "azure_openai", "openai_compatible"] as const;
type Kind = (typeof KINDS)[number];

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/** The language model providers, for administrators: add, edit, test and remove them. */
export function ProvidersSection() {
  const providers = useQuery(providersQuery);
  const [editing, setEditing] = useState<Provider | "new" | null>(null);
  return (
    <section className={card} aria-labelledby="providers-title" data-providers>
      <h2 id="providers-title" className="mb-1 text-lg font-semibold text-ink">
        {s.providers.title}
      </h2>
      <p className="mb-4 text-sm text-ink-muted">{s.providers.body}</p>
      {providers.isPending && <p className="text-sm text-ink-muted">{s.providers.loading}</p>}
      {providers.isError && (
        <p role="alert" className="text-sm text-danger">
          {message(providers.error)}
        </p>
      )}
      {providers.data && providers.data.length === 0 && <p className="mb-4 text-sm text-ink-muted">{s.providers.none}</p>}
      {providers.data && providers.data.length > 0 && (
        <ul className="mb-4 flex flex-col gap-3">
          {providers.data.map((p) => (
            <ProviderRow key={p.id} provider={p} onEdit={() => setEditing(p)} />
          ))}
        </ul>
      )}
      {editing ? (
        <ProviderForm provider={editing === "new" ? undefined : editing} onDone={() => setEditing(null)} />
      ) : (
        <button type="button" className={primary} onClick={() => setEditing("new")} data-action="new-provider">
          {s.providers.form.addTitle}
        </button>
      )}
    </section>
  );
}

function ProviderRow({ provider, onEdit }: { provider: Provider; onEdit: () => void }) {
  const client = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const test = useMutation({ mutationFn: () => testProvider(provider.id) });
  const remove = useMutation({ mutationFn: () => removeProvider(provider.id), onSuccess: () => client.invalidateQueries({ queryKey: providersQueryKey }) });
  return (
    <li className="rounded-control border border-border p-3" data-provider={provider.name}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="font-medium text-ink">{provider.name}</span>
        <span className="flex flex-wrap gap-2 text-xs">
          <span className="text-ink-muted">{s.providers.kinds[provider.kind as Kind] ?? provider.kind}</span>
          {provider.isDefault && <span className="rounded-control bg-accent-subtle px-2 py-0.5 text-ink">{s.providers.isDefault}</span>}
          {!provider.enabled && <span className="rounded-control bg-warning-subtle px-2 py-0.5 text-warning">{s.providers.disabled}</span>}
          <span className={provider.hasKey ? "text-success" : "text-warning"}>{provider.hasKey ? s.providers.keyHeld : s.providers.noKey}</span>
        </span>
      </div>
      <p className="mt-1 break-words text-xs text-ink-muted">
        {s.providers.models}: {provider.models.join(", ")}
      </p>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <button type="button" className={secondary} disabled={test.isPending} onClick={() => test.mutate()} data-action="test-provider">
          {test.isPending ? s.providers.testing : s.providers.test}
        </button>
        <button type="button" className={secondary} onClick={onEdit} data-action="edit-provider">
          {s.providers.edit}
        </button>
        {!confirming ? (
          <button type="button" className={secondary} onClick={() => setConfirming(true)} data-action="remove-provider">
            {s.providers.remove}
          </button>
        ) : (
          <span className="flex flex-wrap items-center gap-2" role="group" aria-label={s.providers.confirmRemove(provider.name)}>
            <span className="text-sm text-ink">{s.providers.confirmRemove(provider.name)}</span>
            <button
              type="button"
              className="h-8 rounded-control bg-danger px-3 text-sm font-medium text-on-danger hover:bg-danger-hover"
              onClick={() => remove.mutate()}
              data-action="confirm-remove-provider"
            >
              {s.providers.confirm}
            </button>
            <button type="button" className={secondary} onClick={() => setConfirming(false)}>
              {s.providers.keep}
            </button>
          </span>
        )}
      </div>
      {test.data && (
        <p role="status" className={`mt-2 text-sm ${test.data.ok ? "text-success" : "text-danger"}`} data-test-result={test.data.ok ? "ok" : "failed"}>
          {test.data.message}
          {test.data.models.length > 0 && ` (${test.data.models.join(", ")})`}
        </p>
      )}
      {(test.isError || remove.isError) && (
        <p role="alert" className="mt-2 text-sm text-danger">
          {message(test.error ?? remove.error)}
        </p>
      )}
    </li>
  );
}

function ProviderForm({ provider, onDone }: { provider?: Provider; onDone: () => void }) {
  const client = useQueryClient();
  const id = useId();
  const [name, setName] = useState(provider?.name ?? "");
  const [kind, setKind] = useState<Kind>((provider?.kind as Kind) ?? "anthropic");
  const [baseURL, setBaseURL] = useState(provider?.baseURL ?? "");
  const [apiVersion, setApiVersion] = useState(provider?.apiVersion ?? "");
  const [models, setModels] = useState(provider?.models.join(", ") ?? "");
  const [defaultModel, setDefaultModel] = useState(provider?.defaultModel ?? "");
  const [isDefault, setIsDefault] = useState(provider?.isDefault ?? false);
  const [enabled, setEnabled] = useState(provider?.enabled ?? true);
  const [apiKey, setApiKey] = useState("");
  const listed = models
    .split(",")
    .map((m) => m.trim())
    .filter(Boolean);
  const save = useMutation({
    mutationFn: () => {
      const body: ProviderRequest = { name, kind, baseURL, apiVersion, models: listed, defaultModel, isDefault, enabled, apiKey };
      return provider ? changeProvider(provider.id, body) : addProvider(body);
    },
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: providersQueryKey });
      onDone();
    },
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    save.mutate();
  }
  const field = (suffix: string) => `${id}-${suffix}`;
  return (
    <form onSubmit={submit} className="flex flex-col gap-3 border-t border-border pt-4" aria-labelledby={field("title")} data-provider-form>
      <h3 id={field("title")} className="text-base font-semibold text-ink">
        {provider ? s.providers.form.editTitle(provider.name) : s.providers.form.addTitle}
      </h3>
      <label htmlFor={field("name")} className="text-sm font-medium text-ink">
        {s.providers.form.name}
      </label>
      <input id={field("name")} required className={input} value={name} onChange={(e) => setName(e.target.value)} />
      <label htmlFor={field("kind")} className="text-sm font-medium text-ink">
        {s.providers.form.kind}
      </label>
      <select id={field("kind")} className={input} value={kind} onChange={(e) => setKind(e.target.value as Kind)}>
        {KINDS.map((k) => (
          <option key={k} value={k}>
            {s.providers.kinds[k]}
          </option>
        ))}
      </select>
      <label htmlFor={field("url")} className="text-sm font-medium text-ink">
        {s.providers.form.baseURL}
      </label>
      <input id={field("url")} type="url" className={input} value={baseURL} onChange={(e) => setBaseURL(e.target.value)} aria-describedby={field("url-hint")} />
      <p id={field("url-hint")} className="text-xs text-ink-muted">
        {s.providers.form.baseURLHint}
      </p>
      {kind === "azure_openai" && (
        <>
          <label htmlFor={field("version")} className="text-sm font-medium text-ink">
            {s.providers.form.apiVersion}
          </label>
          <input id={field("version")} required className={input} value={apiVersion} onChange={(e) => setApiVersion(e.target.value)} />
        </>
      )}
      <label htmlFor={field("models")} className="text-sm font-medium text-ink">
        {s.providers.form.models}
      </label>
      <input
        id={field("models")}
        required
        className={input}
        value={models}
        onChange={(e) => setModels(e.target.value)}
        aria-describedby={field("models-hint")}
      />
      <p id={field("models-hint")} className="text-xs text-ink-muted">
        {s.providers.form.modelsHint}
      </p>
      <label htmlFor={field("default")} className="text-sm font-medium text-ink">
        {s.providers.form.defaultModel}
      </label>
      <select id={field("default")} className={input} value={defaultModel} onChange={(e) => setDefaultModel(e.target.value)}>
        <option value="">{s.providers.form.noDefault}</option>
        {listed.map((m) => (
          <option key={m} value={m}>
            {m}
          </option>
        ))}
      </select>
      <label className="flex items-center gap-2 text-sm text-ink">
        <input type="checkbox" checked={isDefault} onChange={(e) => setIsDefault(e.target.checked)} />
        {s.providers.form.installationDefault}
      </label>
      <label className="flex items-center gap-2 text-sm text-ink">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        {s.providers.form.enabled}
      </label>
      <label htmlFor={field("key")} className="text-sm font-medium text-ink">
        {s.providers.form.apiKey}
      </label>
      <input
        id={field("key")}
        type="password"
        autoComplete="off"
        className={input}
        value={apiKey}
        onChange={(e) => setApiKey(e.target.value)}
        aria-describedby={provider ? field("key-hint") : undefined}
      />
      {provider && (
        <p id={field("key-hint")} className="text-xs text-ink-muted">
          {s.providers.form.apiKeyKeep}
        </p>
      )}
      {save.isError && (
        <p role="alert" className="rounded-control border border-danger bg-danger-subtle px-3 py-2 text-sm text-danger" data-provider-error>
          {message(save.error)}
        </p>
      )}
      <div className="flex gap-2">
        <button type="submit" className={primary} disabled={save.isPending} data-action="save-provider">
          {save.isPending ? s.providers.form.saving : provider ? s.providers.form.save : s.providers.form.add}
        </button>
        <button type="button" className={secondary} onClick={onDone}>
          {s.providers.form.cancel}
        </button>
      </div>
    </form>
  );
}
