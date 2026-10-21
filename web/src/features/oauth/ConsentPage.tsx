import { useMutation, useQuery } from "@tanstack/react-query";
import { useId, useState } from "react";
import { ApiError } from "@/api/client";
import { type AuthorizationRequest, authorizationRequestQuery, decideAuthorization } from "@/api/oauth";
import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";

const card = "max-w-xl rounded-overlay border border-border bg-surface p-5 shadow-1";
const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";
const secondary = "h-9 rounded-control border border-border-strong bg-surface px-4 text-sm font-medium text-ink hover:bg-surface-sunken disabled:opacity-60";

/** Where a person lets an assistant use Rotor as them, or refuses it, and reads why one was refused (#47). */
export function ConsentPage({ request, error }: { request?: string; error?: string }) {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={s.assistants.consentTitle} />
      {error ? (
        <Alert>{s.assistants.refused[error] ?? s.assistants.refusedOther}</Alert>
      ) : request ? (
        <Pending id={request} />
      ) : (
        <Alert>{s.assistants.noRequest}</Alert>
      )}
    </div>
  );
}

function Alert({ children }: { children: string }) {
  return (
    <p role="alert" className="max-w-xl text-sm text-danger" data-consent-refused>
      {children}
    </p>
  );
}

function Pending({ id }: { id: string }) {
  const asked = useQuery(authorizationRequestQuery(id));
  if (asked.isPending) return <p className="text-sm text-ink-muted">{s.assistants.loading}</p>;
  if (asked.error instanceof ApiError && asked.error.status === 404) return <Alert>{s.assistants.expired}</Alert>;
  if (asked.isError) return <Alert>{asked.error.message}</Alert>;
  return <Consent request={asked.data} />;
}

function Consent({ request }: { request: AuthorizationRequest }) {
  const hint = useId();
  // An assistant that asked only to read cannot be allowed more, so the box is ticked and stays so.
  const [readOnly, setReadOnly] = useState(request.readOnly);
  const [leaving, setLeaving] = useState(false);
  const decide = useMutation({
    mutationFn: (allow: boolean) => decideAuthorization(request.id, allow, allow && readOnly),
    onSuccess: (to) => {
      setLeaving(true);
      window.location.assign(to);
    },
  });
  const expired = decide.error instanceof ApiError && decide.error.status === 404;
  const address = request.client.uri || request.client.id;
  return (
    <section className={`${card} flex flex-col gap-4`} aria-labelledby="consent-asks" data-consent={request.client.id}>
      <h2 id="consent-asks" className="text-lg font-semibold text-ink">
        {s.assistants.asks(request.client.name)}
      </h2>
      <p className="text-sm text-ink-muted">{s.assistants.mayDo}</p>
      <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2 text-sm">
        <dt className="text-ink-muted">{s.assistants.address}</dt>
        <dd className="font-mono break-all text-ink" data-consent-address>
          {address}
        </dd>
      </dl>
      <p className="text-sm text-ink-muted" data-consent-kind={request.client.kind}>
        {request.client.kind === "metadata" ? s.assistants.knownByDocument(request.client.id) : s.assistants.knownRegistered}
      </p>
      <p className="text-sm text-ink-muted" data-consent-host>
        {s.assistants.goesBack(request.redirectHost)}
      </p>
      {request.readOnly && <p className="text-sm text-ink-muted">{s.assistants.askedToRead}</p>}
      <div className="flex flex-col gap-1">
        <label className="flex items-center gap-2 text-sm font-medium text-ink">
          <input
            type="checkbox"
            checked={readOnly}
            disabled={request.readOnly}
            onChange={(e) => setReadOnly(e.target.checked)}
            aria-describedby={hint}
            data-consent-read-only
          />
          {s.assistants.readOnly}
        </label>
        <p id={hint} className="text-xs text-ink-muted">
          {s.assistants.readOnlyHint}
        </p>
      </div>
      {decide.isError && <Alert>{expired ? s.assistants.expired : decide.error.message}</Alert>}
      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          className={primary}
          disabled={decide.isPending || leaving || expired}
          onClick={() => decide.mutate(true)}
          data-action="allow-assistant"
        >
          {s.assistants.allow}
        </button>
        <button
          type="button"
          className={secondary}
          disabled={decide.isPending || leaving || expired}
          onClick={() => decide.mutate(false)}
          data-action="deny-assistant"
        >
          {s.assistants.deny}
        </button>
        <p role="status" className="text-sm text-ink-muted empty:hidden">
          {leaving ? s.assistants.leaving : ""}
        </p>
      </div>
    </section>
  );
}
