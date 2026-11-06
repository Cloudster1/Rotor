import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { type IngestKey, type IngestKeySecret, ingestKeysQuery, ingestKeysQueryKey, revokeIngestKey, rotateIngestKey } from "@/api/ingestKeys";
import { MAX_INGEST_KEYS } from "@/config";
import { DATE_AND_TIME, formatDateTime } from "@/lib/dates";
import { s } from "@/strings";
import { IngestKeyForm, NewKeySecret } from "./IngestKeyForm";
import { danger, item, primary, secondary } from "./styles";
import { SettingsCard } from "./ui";

const t = s.traceDestinations.ingestKeys;
const at = (when: string) => formatDateTime(when, DATE_AND_TIME);

/** The keys services send spans with (#258): made, rotated and revoked by administrators, each shown once. */
export function IngestKeysSection() {
  const keys = useQuery(ingestKeysQuery);
  const [making, setMaking] = useState(false);
  const [shown, setShown] = useState<IngestKeySecret | null>(null);
  const count = keys.data?.length ?? 0;
  const full = count >= MAX_INGEST_KEYS;
  return (
    <SettingsCard titleId="ingest-keys-title" title={t.title} description={t.body} wide data-ingest-keys>
      {keys.isPending && <p className="text-sm text-ink-muted">{t.loading}</p>}
      {keys.isError && (
        <p role="alert" className="text-sm text-danger">
          {keys.error.message}
        </p>
      )}
      {keys.data && (
        <div className="flex flex-col gap-3">
          {shown && <NewKeySecret made={shown} onDone={() => setShown(null)} />}
          {count === 0 && <p className="text-sm text-ink-muted">{t.none}</p>}
          {count > 0 && (
            <ul className="flex flex-col gap-3">
              {keys.data.map((key) => (
                <KeyRow key={key.id} ingestKey={key} onRotated={setShown} />
              ))}
            </ul>
          )}
          {making ? (
            <IngestKeyForm
              onMade={(made) => {
                setMaking(false);
                setShown(made);
              }}
              onCancel={() => setMaking(false)}
            />
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <button type="button" className={primary} disabled={full} onClick={() => setMaking(true)}>
                {t.make}
              </button>
              {full && <span className="text-sm text-ink-muted">{t.full(MAX_INGEST_KEYS)}</span>}
            </div>
          )}
        </div>
      )}
    </SettingsCard>
  );
}

function KeyRow({ ingestKey, onRotated }: { ingestKey: IngestKey; onRotated: (made: IngestKeySecret) => void }) {
  const client = useQueryClient();
  const [confirming, setConfirming] = useState<"rotate" | "revoke" | null>(null);
  const refresh = () => client.invalidateQueries({ queryKey: ingestKeysQueryKey });
  const rotate = useMutation({
    mutationFn: () => rotateIngestKey(ingestKey.id),
    onSuccess: async (made) => {
      setConfirming(null);
      await refresh();
      onRotated(made);
    },
  });
  const revoke = useMutation({ mutationFn: () => revokeIngestKey(ingestKey.id), onSuccess: refresh });
  const confirmText = confirming === "rotate" ? t.confirmRotate(ingestKey.name) : t.confirmRevoke(ingestKey.name);
  const pending = rotate.isPending || revoke.isPending;
  return (
    <li className={item} data-ingest-key={ingestKey.name}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="font-medium text-ink">{ingestKey.name}</span>
        {ingestKey.expired && <span className="rounded-control bg-warning-subtle px-2 py-0.5 text-xs text-ink">{t.expired}</span>}
      </div>
      <ul className="mt-1 flex flex-col gap-0.5 text-xs text-ink-muted">
        <li className="break-all">{t.sendsAs(ingestKey.account.name, ingestKey.account.email)}</li>
        <li data-ingest-key-hint>{t.endsIn(ingestKey.hint)}</li>
        <li>{ingestKey.expiresAt ? (ingestKey.expired ? t.expiredAt : t.expiresAt)(at(ingestKey.expiresAt)) : t.noExpiry}</li>
        <li data-ingest-key-used>{ingestKey.lastUsedAt ? t.lastUsed(at(ingestKey.lastUsedAt)) : t.neverUsed}</li>
        <li>{t.madeBy(ingestKey.createdBy, at(ingestKey.createdAt))}</li>
        {ingestKey.rotatedAt && <li>{t.rotatedAt(at(ingestKey.rotatedAt))}</li>}
      </ul>
      {(rotate.isError || revoke.isError) && (
        <p role="alert" className="mt-2 text-sm text-danger">
          {(rotate.error ?? revoke.error)?.message}
        </p>
      )}
      <div className="mt-2 flex flex-wrap items-center gap-2">
        {confirming === null ? (
          <>
            {!ingestKey.expired && (
              <button type="button" className={secondary} onClick={() => setConfirming("rotate")} aria-label={`${t.rotate} ${ingestKey.name}`}>
                {t.rotate}
              </button>
            )}
            <button type="button" className={secondary} onClick={() => setConfirming("revoke")} aria-label={`${t.revoke} ${ingestKey.name}`}>
              {t.revoke}
            </button>
          </>
        ) : (
          <span className="flex flex-wrap items-center gap-2" role="group" aria-label={confirmText}>
            <span className="text-sm text-ink">{confirmText}</span>
            <button type="button" className={danger} disabled={pending} onClick={() => (confirming === "rotate" ? rotate.mutate() : revoke.mutate())}>
              {confirming === "rotate" ? t.rotateIt : t.revokeIt}
            </button>
            <button type="button" className={secondary} onClick={() => setConfirming(null)}>
              {t.keep}
            </button>
          </span>
        )}
      </div>
    </li>
  );
}
