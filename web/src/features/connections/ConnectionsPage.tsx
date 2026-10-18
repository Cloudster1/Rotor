import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { type Connection, connectionsQuery, connectionsQueryKey, disconnect, startConnection } from "@/api/connections";
import { type Tool, toolName, toolsQuery } from "@/api/tools";
import { EmptyState, PageHeader } from "@/features/shell/EmptyState";
import { Icon } from "@/features/shell/icons";
import { s } from "@/strings";

const primary = "h-9 rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover disabled:opacity-60";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";

/** What the callback came back with, from the page's address. */
export interface ConnectionsSearch {
  connected?: string;
  error?: string;
  tool?: string;
}

function reason(error: string): string {
  const known: Record<string, string> = s.connections.errors;
  return known[error] ?? s.connections.errors.tool;
}

function when(at: string | null | undefined): string {
  if (!at) return s.connections.never;
  return new Date(at).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

/** Each tool Rotor knows, with the person's connection to it: connect, see what was allowed, disconnect. */
export function ConnectionsPage({ search }: { search: ConnectionsSearch }) {
  const tools = useQuery(toolsQuery);
  const connections = useQuery(connectionsQuery);
  const byTool = new Map((connections.data ?? []).map((c) => [c.tool.id, c]));
  const named = (id?: string) => tools.data?.find((t) => t.id === id);
  const connectedTool = named(search.connected);
  const Glyph = Icon.Connection;
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={s.places.connections} />
      {search.connected && connectedTool && (
        <p role="status" className="max-w-xl rounded-control border border-success bg-success-subtle px-3 py-2 text-sm text-success" data-connected>
          {s.connections.done(toolName(connectedTool))}
        </p>
      )}
      {search.error && (
        <p role="alert" className="max-w-xl rounded-control border border-danger bg-danger-subtle px-3 py-2 text-sm text-danger" data-connect-error>
          {reason(search.error)}
        </p>
      )}
      {(tools.isPending || connections.isPending) && <p className="text-sm text-ink-muted">{s.connections.loading}</p>}
      {(tools.isError || connections.isError) && (
        <p role="alert" className="text-sm text-danger">
          {s.connections.unreachable}
        </p>
      )}
      {tools.data && tools.data.length === 0 && <EmptyState icon={<Glyph />} title={s.connections.emptyTitle} description={s.connections.emptyBody} />}
      {tools.data && tools.data.length > 0 && connections.data && (
        <>
          <p className="max-w-xl text-sm text-ink-muted">{s.connections.body}</p>
          <ul className="flex max-w-xl flex-col gap-4">
            {tools.data.map((tool) => (
              <ToolConnection key={tool.id} tool={tool} connection={byTool.get(tool.id)} />
            ))}
          </ul>
        </>
      )}
    </div>
  );
}

function ToolConnection({ tool, connection }: { tool: Tool; connection?: Connection }) {
  const client = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const name = toolName(tool);
  const connect = useMutation({
    mutationFn: () => startConnection(tool.id),
    onSuccess: (authorizeURL) => window.location.assign(authorizeURL),
  });
  const end = useMutation({
    mutationFn: () => disconnect(tool.id),
    onSuccess: () => {
      setConfirming(false);
      return client.invalidateQueries({ queryKey: connectionsQueryKey });
    },
  });
  const unavailable = tool.status !== "ready";
  const status = unavailable ? "unavailable" : (connection?.status ?? "none");
  const titleId = `connection-${tool.id}`;
  return (
    <li className="rounded-overlay border border-border bg-surface p-5 shadow-1" aria-labelledby={titleId} data-connection={tool.name} data-status={status}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 id={titleId} className="text-lg font-semibold text-ink">
          {name}
        </h2>
        <span className="text-sm text-ink-muted">{s.connections.status[status]}</span>
      </div>
      <p className="mb-3 break-all text-sm text-ink-muted">{tool.baseURL}</p>
      {connection && (
        <dl className="mb-3 grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
          <dt className="text-ink-muted">{s.connections.scopes}</dt>
          <dd className="text-ink" data-scopes>
            {connection.scopes.join(" ")}
          </dd>
          <dt className="text-ink-muted">{s.connections.connectedAt}</dt>
          <dd className="text-ink">
            <time dateTime={connection.connectedAt}>{when(connection.connectedAt)}</time>
          </dd>
          <dt className="text-ink-muted">{s.connections.lastUsed}</dt>
          <dd className="text-ink">{when(connection.lastUsedAt)}</dd>
        </dl>
      )}
      {unavailable && <p className="mb-3 text-sm text-ink-muted">{s.connections.needsRegistration}</p>}
      <div className="flex flex-wrap items-center gap-2">
        {!unavailable && (!connection || connection.status === "reconnect") && (
          <button type="button" className={primary} disabled={connect.isPending} onClick={() => connect.mutate()} data-action="connect">
            {connection ? s.connections.connectAgain(name) : s.connections.connect(name)}
          </button>
        )}
        {connection && !confirming && (
          <button type="button" className={secondary} onClick={() => setConfirming(true)} data-action="disconnect">
            {s.connections.disconnect(name)}
          </button>
        )}
        {connection && confirming && (
          <span className="flex flex-wrap items-center gap-2" role="group" aria-label={s.connections.confirmDisconnect(name)}>
            <span className="text-sm text-ink">{s.connections.confirmDisconnect(name)}</span>
            <button
              type="button"
              className="h-8 rounded-control bg-danger px-3 text-sm font-medium text-on-danger hover:bg-danger-hover disabled:opacity-60"
              disabled={end.isPending}
              onClick={() => end.mutate()}
              data-action="confirm-disconnect"
            >
              {s.connections.confirm}
            </button>
            <button type="button" className={secondary} onClick={() => setConfirming(false)}>
              {s.connections.keep}
            </button>
          </span>
        )}
      </div>
      {(connect.isError || end.isError) && (
        <p role="alert" className="mt-2 text-sm text-danger">
          {(connect.error ?? end.error)?.message}
        </p>
      )}
    </li>
  );
}
