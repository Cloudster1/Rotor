import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { ApiError } from "@/api/client";
import { type SingleSignOn, saveSingleSignOn, singleSignOnQuery, singleSignOnQueryKey } from "@/api/sso";
import { LOGIN_PATH } from "@/config";
import { card, input, primary, secondary } from "@/features/settings/styles";
import { s } from "@/strings";
import { Refusal } from "./ui";

/** The Single sign-on tab of Access: the provider people sign in through, changed live. Ported from Armature's. */
export function ProviderSettings() {
  const sso = useQuery(singleSignOnQuery);
  if (sso.isPending) return <p className="text-sm text-ink-muted">{s.sso.loading}</p>;
  if (sso.isError) return <Refusal error={sso.error} />;
  // The form starts afresh once there is a provider; a save keeps what was typed, which is what the server now holds.
  return <ProviderForm key={sso.data.provider ? "held" : "none"} current={sso.data} />;
}

/** The fields the server answers with an error of their own, so each says what to fix beside it. */
type Field = "issuer" | "clientId" | "clientSecret" | "groupsClaim";

function fieldsOf(error: unknown): Partial<Record<Field, string>> {
  return error instanceof ApiError ? error.fields : {};
}

function ProviderForm({ current }: { current: SingleSignOn }) {
  const id = useId();
  const client = useQueryClient();
  const provider = current.provider;
  const [issuer, setIssuer] = useState(provider?.issuer ?? "");
  const [clientId, setClientId] = useState(provider?.clientId ?? "");
  const [secret, setSecret] = useState("");
  const [claim, setClaim] = useState(provider?.groupsClaim ?? "groups");
  const [admit, setAdmit] = useState(provider?.admitByGroup ?? false);
  // Saving turns it on, as Armature's form does; turning off sends what is held, with no new secret.
  const save = useMutation({
    mutationFn: (enabled: boolean) =>
      saveSingleSignOn({ issuer, clientId, clientSecret: (enabled && secret) || undefined, groupsClaim: claim, admitByGroup: admit, enabled }),
    onSuccess: (next) => {
      setSecret("");
      client.setQueryData(singleSignOnQueryKey, next);
    },
  });
  const wrong = fieldsOf(save.error);
  const field = (suffix: string) => `${id}-${suffix}`;
  const submit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate(true);
  };
  const signInAt = `${window.location.origin}${LOGIN_PATH}`;

  function text({ name, label, hint, value, onChange, ...rest }: TextProps) {
    const hintId = hint || wrong[name] ? field(`${name}-hint`) : undefined;
    return (
      <div className="flex flex-col gap-1">
        <label htmlFor={field(name)} className="text-sm font-medium text-ink">
          {label}
        </label>
        <input
          id={field(name)}
          className={input}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          aria-describedby={hintId}
          aria-invalid={wrong[name] ? true : undefined}
          data-sso-field={name}
          {...rest}
        />
        {hintId && (
          <p id={hintId} className={`text-xs ${wrong[name] ? "text-danger" : "text-ink-muted"}`}>
            {wrong[name] ?? hint}
          </p>
        )}
      </div>
    );
  }

  return (
    <form className={`${card} flex flex-col gap-4`} onSubmit={submit} noValidate aria-label={s.access.tabs.sso} data-sso>
      <p className="text-sm text-ink-muted">{s.sso.intro}</p>
      <div className="grid gap-3 sm:grid-cols-2">
        {text({
          name: "issuer",
          label: s.sso.issuer,
          hint: s.sso.issuerHint,
          value: issuer,
          onChange: setIssuer,
          placeholder: "https://id.example.com/realms/dynamo",
          inputMode: "url",
        })}
        {text({ name: "clientId", label: s.sso.clientId, value: clientId, onChange: setClientId, autoComplete: "off" })}
        {text({
          name: "clientSecret",
          label: s.sso.secret,
          hint: provider?.hasSecret ? s.sso.secretStored : s.sso.secretMissing,
          value: secret,
          onChange: setSecret,
          type: "password",
          autoComplete: "new-password",
          placeholder: provider?.hasSecret ? s.sso.secretStored : undefined,
        })}
        {text({ name: "groupsClaim", label: s.sso.groupsClaim, hint: s.sso.groupsClaimHint, value: claim, onChange: setClaim, autoComplete: "off" })}
      </div>
      <div className="flex flex-col gap-1">
        <label className="flex items-center gap-2 text-sm font-medium text-ink">
          <input type="checkbox" checked={admit} onChange={(event) => setAdmit(event.target.checked)} aria-describedby={field("admit")} data-admit-by-group />
          {s.sso.admit}
        </label>
        <p id={field("admit")} className="text-xs text-ink-muted">
          {s.sso.admitDetail}
        </p>
      </div>
      <Refusal error={save.error} />
      {save.isSuccess && (
        <p role="status" className="text-sm text-ink" data-sso-saved>
          {s.sso.saved}
        </p>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <button type="submit" className={primary} disabled={save.isPending} data-action="save-sso">
          {save.isPending ? s.sso.saving : provider ? s.sso.save : s.sso.setUp}
        </button>
        {provider?.enabled && current.canTurnOff && (
          <button type="button" className={secondary} disabled={save.isPending} onClick={() => save.mutate(false)} data-action="turn-off-sso">
            {s.sso.turnOff}
          </button>
        )}
      </div>
      {provider && (
        <div className="flex flex-col gap-1 text-sm text-ink-muted">
          <p>
            {provider.enabled ? s.sso.signInAt : s.sso.offAddress}{" "}
            <code className="break-all text-ink" data-sso-start>
              {signInAt}
            </code>
          </p>
          <p>
            {s.sso.redirect}{" "}
            <code className="break-all text-ink" data-sso-redirect>
              {current.redirectUrl}
            </code>
          </p>
          {provider.changedBy && <p>{s.sso.changedBy(provider.changedBy)}</p>}
        </div>
      )}
      {/* Turning it off is offered only where another way in remains; here the page says why it is not. */}
      {provider?.enabled && !current.canTurnOff && (
        <p className="text-sm text-ink-muted" data-sso-only-way-in>
          {s.sso.cannotTurnOff}
        </p>
      )}
    </form>
  );
}

interface TextProps {
  name: Field;
  label: string;
  hint?: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  type?: string;
  autoComplete?: string;
  inputMode?: "url";
}
