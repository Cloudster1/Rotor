import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useEffect, useId, useState } from "react";
import { ApiError } from "@/api/client";
import { createIngestKey, type IngestKeySecret, ingestKeysQueryKey } from "@/api/ingestKeys";
import { COPIED_NOTICE_MS, MAX_INGEST_KEY_NAME, OTLP_HEADERS_VARIABLE } from "@/config";
import { s } from "@/strings";
import { input, primary, secondary } from "./styles";

const t = s.traceDestinations.ingestKeys;

function Field({ id, label, hint, error, children }: { id: string; label: string; hint: string; error?: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-sm font-medium text-ink">
        {label}
      </label>
      {children}
      <p id={`${id}-hint`} className={`text-xs ${error ? "text-danger" : "text-ink-muted"}`}>
        {error ?? hint}
      </p>
    </div>
  );
}

/** Makes a key; onMade gets the answer, the one that holds the key. */
export function IngestKeyForm({ onMade, onCancel }: { onMade: (made: IngestKeySecret) => void; onCancel: () => void }) {
  const id = useId();
  const client = useQueryClient();
  const [name, setName] = useState("");
  const [account, setAccount] = useState("");
  const [expires, setExpires] = useState("");
  const make = useMutation({
    mutationFn: () =>
      createIngestKey({
        name,
        accountEmail: account.trim() || undefined,
        // The field is the browser's local time; the server takes an instant.
        expiresAt: expires ? new Date(expires).toISOString() : undefined,
      }),
    onSuccess: async (made) => {
      await client.invalidateQueries({ queryKey: ingestKeysQueryKey });
      onMade(made);
    },
  });
  const fields = make.error instanceof ApiError ? make.error.fields : {};
  const submit = (event: FormEvent) => {
    event.preventDefault();
    make.mutate();
  };
  const field = (suffix: string) => `${id}-${suffix}`;
  return (
    <form className="flex flex-col gap-3 rounded-control border border-border p-3" onSubmit={submit} noValidate aria-label={t.formTitle} data-ingest-key-form>
      <Field id={field("name")} label={t.name} hint={t.nameHint} error={fields.name}>
        <input
          id={field("name")}
          className={input}
          value={name}
          maxLength={MAX_INGEST_KEY_NAME}
          onChange={(e) => setName(e.target.value)}
          aria-describedby={`${field("name")}-hint`}
          aria-invalid={fields.name ? true : undefined}
        />
      </Field>
      <Field id={field("account")} label={t.account} hint={t.accountHint} error={fields.accountEmail}>
        <input
          id={field("account")}
          className={input}
          type="email"
          value={account}
          onChange={(e) => setAccount(e.target.value)}
          aria-describedby={`${field("account")}-hint`}
          aria-invalid={fields.accountEmail ? true : undefined}
        />
      </Field>
      <Field id={field("expires")} label={t.expires} hint={t.expiresHint} error={fields.expiresAt}>
        <input
          id={field("expires")}
          className={input}
          type="datetime-local"
          value={expires}
          onChange={(e) => setExpires(e.target.value)}
          aria-describedby={`${field("expires")}-hint`}
          aria-invalid={fields.expiresAt ? true : undefined}
        />
      </Field>
      {make.isError && (
        <p role="alert" className="text-sm text-danger">
          {make.error.message}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <button type="submit" className={primary} disabled={make.isPending}>
          {make.isPending ? t.saving : t.save}
        </button>
        <button type="button" className={secondary} onClick={onCancel}>
          {t.cancel}
        </button>
      </div>
    </form>
  );
}

/** The one time a key is shown: to copy into the service's exporter, then gone. */
export function NewKeySecret({ made, onDone }: { made: IngestKeySecret; onDone: () => void }) {
  const id = useId();
  const [copied, setCopied] = useState<"" | "done" | "failed">("");
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(""), COPIED_NOTICE_MS);
    return () => clearTimeout(timer);
  }, [copied]);
  async function copy() {
    try {
      await navigator.clipboard.writeText(made.secret);
      setCopied("done");
    } catch {
      setCopied("failed");
    }
  }
  return (
    <div className="flex flex-col gap-2 rounded-control border border-warning bg-warning-subtle p-3" data-ingest-key-secret>
      <p className="text-sm font-medium text-ink">{t.shownOnce}</p>
      <label htmlFor={`${id}-secret`} className="text-sm text-ink">
        {t.secretLabel(made.key.name)}
      </label>
      <input id={`${id}-secret`} className={`${input} font-mono`} readOnly value={made.secret} onFocus={(e) => e.currentTarget.select()} />
      <p className="text-xs text-ink">{t.setIt(OTLP_HEADERS_VARIABLE)}</p>
      <code className="break-all rounded-control bg-surface px-2 py-1 text-xs text-ink">{`${OTLP_HEADERS_VARIABLE}=Authorization=Bearer%20${made.secret}`}</code>
      <div className="flex flex-wrap items-center gap-2">
        <button type="button" className={secondary} onClick={copy}>
          {t.copy}
        </button>
        <button type="button" className={primary} onClick={onDone}>
          {t.saved}
        </button>
        <span role="status" className="text-sm text-ink-muted">
          {copied === "done" ? t.copied : copied === "failed" ? t.copyFailed : ""}
        </span>
      </div>
    </div>
  );
}
