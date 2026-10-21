import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { endGrant, type Grant, grantsQuery, grantsQueryKey } from "@/api/oauth";
import { s } from "@/strings";

const card = "max-w-xl rounded-overlay border border-border bg-surface p-5 shadow-1";
export const danger =
  "h-8 rounded-control border border-border-strong bg-surface px-3 text-sm font-medium text-danger hover:bg-surface-sunken disabled:opacity-60";

export function when(at: string): string {
  return new Date(at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/** What a grant may do and when it was used, in a line. */
export function grantLine(grant: Grant): string {
  const used = grant.lastUsedAt ? s.assistants.lastUsed(when(grant.lastUsedAt)) : s.assistants.neverUsed;
  const may = grant.readOnly ? s.assistants.readsOnly : s.assistants.actsForYou;
  return `${may}. ${s.assistants.connectedOn(when(grant.createdAt))}, ${used}, ${s.assistants.returnsTo(grant.redirectHost)}.`;
}

/** The assistants a person connected over MCP, each disconnected at once (#47). */
export function AssistantsSection() {
  const grants = useQuery(grantsQuery);
  const client = useQueryClient();
  const end = useMutation({
    mutationFn: (id: string) => endGrant(id),
    onSettled: () => client.invalidateQueries({ queryKey: grantsQueryKey }),
  });
  return (
    <section className={card} aria-labelledby="assistants-title" data-assistants>
      <h2 id="assistants-title" className="mb-1 text-lg font-semibold text-ink">
        {s.assistants.sectionTitle}
      </h2>
      <p className="mb-3 text-sm text-ink-muted">{s.assistants.sectionBody}</p>
      {grants.isPending && <p className="text-sm text-ink-muted">{s.assistants.loading}</p>}
      {grants.isError && (
        <p role="alert" className="text-sm text-danger">
          {grants.error.message}
        </p>
      )}
      {grants.data?.length === 0 && <p className="text-sm text-ink-muted">{s.assistants.none}</p>}
      {end.isError && (
        <p role="alert" className="mb-2 text-sm text-danger">
          {end.error.message}
        </p>
      )}
      {!!grants.data?.length && (
        <ul className="flex flex-col divide-y divide-border">
          {grants.data.map((grant) => (
            <li key={grant.id} className="flex items-start justify-between gap-3 py-2" data-grant={grant.client.name}>
              <div className="min-w-0">
                <p className="text-sm font-medium text-ink">{grant.client.name}</p>
                <p className="text-xs text-ink-muted">{grantLine(grant)}</p>
              </div>
              <button
                type="button"
                className={danger}
                disabled={end.isPending}
                aria-label={s.assistants.disconnectNamed(grant.client.name)}
                onClick={() => end.mutate(grant.id)}
                data-action="disconnect-assistant"
              >
                {end.isPending && end.variables === grant.id ? s.assistants.disconnecting : s.assistants.disconnect}
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
