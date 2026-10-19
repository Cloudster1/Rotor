import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import type { Provider } from "@/api/providers";
import { type ModelPrice, pricesQuery, setPrices, usageQueryKey } from "@/api/usage";
import { s } from "@/strings";

const input = "h-9 w-full rounded-control border border-border-strong bg-surface px-3 text-sm text-ink";
const primary = "h-8 rounded-control bg-accent px-3 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised";

type Row = { input: string; output: string };

/** A provider's prices per model, in US dollars per million tokens; a model left blank counts tokens only. */
export function PricesPanel({ provider, onClose }: { provider: Provider; onClose: () => void }) {
  const prices = useQuery(pricesQuery(provider.id));
  if (!prices.data) {
    return prices.isError ? (
      <p role="alert" className="mt-2 text-sm text-danger">
        {prices.error.message}
      </p>
    ) : null;
  }
  return <PricesForm provider={provider} current={prices.data} onClose={onClose} />;
}

function PricesForm({ provider, current, onClose }: { provider: Provider; current: ModelPrice[]; onClose: () => void }) {
  const id = useId();
  const client = useQueryClient();
  const [rows, setRows] = useState<Record<string, Row>>(() =>
    Object.fromEntries(
      provider.models.map((m) => {
        const p = current.find((c) => c.model === m);
        return [m, { input: p ? String(p.inputPerMillion) : "", output: p ? String(p.outputPerMillion) : "" }];
      }),
    ),
  );
  const row = (m: string): Row => rows[m] ?? { input: "", output: "" };
  const save = useMutation({
    mutationFn: () =>
      setPrices(
        provider.id,
        provider.models
          .filter((m) => row(m).input.trim() !== "" || row(m).output.trim() !== "")
          .map((m) => ({ model: m, inputPerMillion: Number(row(m).input || 0), outputPerMillion: Number(row(m).output || 0) })),
      ),
    onSuccess: () => client.invalidateQueries({ queryKey: usageQueryKey }),
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate();
  };
  return (
    <form
      className="mt-3 flex flex-col gap-3 rounded-control border border-border p-3"
      noValidate
      onSubmit={submit}
      aria-labelledby={`${id}-title`}
      data-prices
    >
      <h3 id={`${id}-title`} className="text-sm font-semibold text-ink">
        {s.usage.pricesTitle(provider.name)}
      </h3>
      <p className="text-xs text-ink-muted">{s.usage.pricesBody}</p>
      {provider.models.map((m) => (
        <div key={m} className="grid gap-2 sm:grid-cols-2">
          {(["input", "output"] as const).map((side) => (
            <div key={side} className="flex flex-col gap-1">
              <label htmlFor={`${id}-${m}-${side}`} className="text-xs text-ink">
                {side === "input" ? s.usage.inputPrice(m) : s.usage.outputPrice(m)}
              </label>
              <input
                id={`${id}-${m}-${side}`}
                className={input}
                type="number"
                min={0}
                step="any"
                value={row(m)[side]}
                onChange={(e) => setRows({ ...rows, [m]: { ...row(m), [side]: e.target.value } })}
                data-price={`${m}-${side}`}
              />
            </div>
          ))}
        </div>
      ))}
      <div className="flex gap-2">
        <button type="submit" className={primary} disabled={save.isPending} data-action="save-prices">
          {s.usage.savePrices}
        </button>
        <button type="button" className={secondary} onClick={onClose}>
          {s.usage.close}
        </button>
      </div>
      {save.isSuccess && (
        <p role="status" className="text-sm text-ink">
          {s.usage.savedPrices}
        </p>
      )}
      {save.isError && (
        <p role="alert" className="text-sm text-danger">
          {save.error.message}
        </p>
      )}
    </form>
  );
}
