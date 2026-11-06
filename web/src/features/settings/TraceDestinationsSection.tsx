import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import {
  removeTraceDestination,
  type TraceDestination,
  traceDestinationsQuery,
  traceDestinationsQueryKey,
  testTraceDestination,
} from "@/api/traceDestinations";
import { MAX_TRACE_DESTINATIONS } from "@/config";
import { DATE_AND_TIME, formatDateTime } from "@/lib/dates";
import { s } from "@/strings";
import { DESTINATION_PRESETS } from "./destinationPresets";
import { DestinationForm, type DestinationStart } from "./DestinationForm";
import { IngestKeysSection } from "./IngestKeysSection";
import { danger, item, primary, secondary } from "./styles";
import { TraceKeepingCard } from "./TraceKeepingCard";
import { SettingsCard } from "./ui";

const t = s.traceDestinations;

/** The Traces admin page: how long traces and their text are kept (#242), the OTLP tools they are also sent to (#246),
 * and the keys services send spans with (#258). */
export function TraceDestinationsSection() {
  return (
    <>
      <TraceKeepingCard />
      <Destinations />
      <IngestKeysSection />
    </>
  );
}

function Destinations() {
  const destinations = useQuery(traceDestinationsQuery);
  const [starting, setStarting] = useState<DestinationStart | null>(null);
  const count = destinations.data?.length ?? 0;
  const full = count >= MAX_TRACE_DESTINATIONS;
  return (
    <SettingsCard titleId="trace-destinations-title" title={t.title} description={t.body} wide data-trace-destinations>
      {destinations.isPending && <p className="text-sm text-ink-muted">{t.loading}</p>}
      {destinations.isError && (
        <p role="alert" className="text-sm text-danger" data-destinations-error>
          {destinations.error.message}
        </p>
      )}
      {destinations.data && (
        <div className="flex flex-col gap-3">
          {count === 0 && <p className="text-sm text-ink-muted">{t.none}</p>}
          {count > 0 && (
            <ul className="flex flex-col gap-3">
              {destinations.data.map((destination) =>
                starting && "destination" in starting && starting.destination.id === destination.id ? (
                  <li key={destination.id}>
                    <DestinationForm start={starting} onDone={() => setStarting(null)} />
                  </li>
                ) : (
                  <DestinationRow key={destination.id} destination={destination} onEdit={() => setStarting({ destination })} />
                ),
              )}
            </ul>
          )}
          {starting && !("destination" in starting) ? (
            <NewDestination start={starting} onStart={setStarting} onDone={() => setStarting(null)} />
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <button type="button" className={primary} disabled={full} onClick={() => setStarting({})} data-action="new-destination">
                {t.add}
              </button>
              {full && <span className="text-sm text-ink-muted">{t.full(MAX_TRACE_DESTINATIONS)}</span>}
            </div>
          )}
        </div>
      )}
    </SettingsCard>
  );
}

function NewDestination({ start, onStart, onDone }: { start: DestinationStart; onStart: (next: DestinationStart) => void; onDone: () => void }) {
  const preset = "preset" in start ? start.preset : undefined;
  // A new key remounts the form, since a preset only fills what the form starts from.
  return (
    <div className="flex flex-col gap-3">
      <h3 className="text-base font-semibold text-ink">{t.addTitle}</h3>
      <div role="group" aria-label={t.presetsLabel} className="flex flex-wrap items-center gap-2">
        <span className="text-sm text-ink">{t.presetsLabel}</span>
        <button type="button" className={secondary} aria-pressed={!preset} onClick={() => onStart({})} data-preset="blank">
          {t.presetBlank}
        </button>
        {DESTINATION_PRESETS.map((each) => (
          <button
            key={each.key}
            type="button"
            className={secondary}
            aria-pressed={preset?.key === each.key}
            onClick={() => onStart({ preset: each })}
            data-preset={each.key}
          >
            {each.name}
          </button>
        ))}
      </div>
      <p className="text-xs text-ink-muted">{preset ? `${t.presetsHint} ${preset.hint}` : t.presetsHint}</p>
      <DestinationForm key={preset?.key ?? "blank"} start={start} onDone={onDone} />
    </div>
  );
}

function Badge({ children, tone }: { children: string; tone: "ok" | "warn" | "plain" }) {
  const colours = { ok: "bg-accent-subtle text-ink", warn: "bg-warning-subtle text-ink", plain: "bg-surface-sunken text-ink-muted" };
  return <span className={`rounded-control px-2 py-0.5 text-xs ${colours[tone]}`}>{children}</span>;
}

function DestinationRow({ destination, onEdit }: { destination: TraceDestination; onEdit: () => void }) {
  const client = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const test = useMutation({ mutationFn: () => testTraceDestination(destination.id) });
  const remove = useMutation({
    mutationFn: () => removeTraceDestination(destination.id),
    onSuccess: () => client.invalidateQueries({ queryKey: traceDestinationsQueryKey }),
  });
  const at = destination.contentAcknowledgedAt ? formatDateTime(destination.contentAcknowledgedAt, DATE_AND_TIME) : null;
  const confirmText = t.confirmRemove(destination.name);
  return (
    <li className={item} data-destination={destination.name}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <span className="font-medium text-ink">{destination.name}</span>
        <span className="flex flex-wrap gap-2">
          <Badge tone={destination.enabled ? "ok" : "warn"}>{destination.enabled ? t.on : t.off}</Badge>
          {destination.carriedOver && <Badge tone="plain">{t.carriedOver}</Badge>}
        </span>
      </div>
      <ul className="mt-1 flex flex-col gap-0.5 text-xs text-ink-muted">
        <li className="break-all">{destination.endpoint}</li>
        <li data-destination-text={destination.content ? "on" : "off"}>{destination.content ? t.text(destination.contentAcknowledgedBy, at) : t.noText}</li>
        <li data-destination-headers>{destination.headerNames.length > 0 ? t.headersHeld(destination.headerNames) : t.noHeaders}</li>
        <li className="break-all">{destination.traceLink ? t.link(destination.traceLink) : t.noLink}</li>
        {destination.changedBy && <li>{t.changedBy(destination.changedBy)}</li>}
      </ul>
      {test.data && (
        <p
          role="status"
          className={`mt-2 text-sm ${test.data.delivered ? "text-success" : "text-danger"}`}
          data-destination-test={test.data.delivered ? "delivered" : "failed"}
        >
          {test.data.delivered ? t.testDelivered(test.data.message) : t.testFailed(test.data.status, test.data.message)}
        </p>
      )}
      {(test.isError || remove.isError) && (
        <p role="alert" className="mt-2 text-sm text-danger">
          {(test.error ?? remove.error)?.message}
        </p>
      )}
      <div className="mt-2 flex flex-wrap items-center gap-2">
        <button type="button" className={secondary} onClick={onEdit} aria-label={`${t.edit} ${destination.name}`} data-action="edit-destination">
          {t.edit}
        </button>
        <button
          type="button"
          className={secondary}
          disabled={test.isPending}
          onClick={() => test.mutate()}
          aria-label={`${t.test} ${destination.name}`}
          data-action="test-destination"
        >
          {test.isPending ? t.testing : t.test}
        </button>
        {!confirming ? (
          <button
            type="button"
            className={secondary}
            onClick={() => setConfirming(true)}
            aria-label={`${t.remove} ${destination.name}`}
            data-action="remove-destination"
          >
            {t.remove}
          </button>
        ) : (
          <span className="flex flex-wrap items-center gap-2" role="group" aria-label={confirmText}>
            <span className="text-sm text-ink">{confirmText}</span>
            <button type="button" className={danger} disabled={remove.isPending} onClick={() => remove.mutate()} data-action="confirm-remove-destination">
              {t.confirm}
            </button>
            <button type="button" className={secondary} onClick={() => setConfirming(false)}>
              {t.keep}
            </button>
          </span>
        )}
      </div>
    </li>
  );
}
