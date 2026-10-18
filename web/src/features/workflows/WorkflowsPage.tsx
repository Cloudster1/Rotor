import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { archiveWorkflow, type WorkflowSummary, workflowsQuery, workflowsQueryKey } from "@/api/workflows";
import { RunDialog } from "@/features/runs/RunDialog";
import { EmptyState, PageHeader } from "@/features/shell/EmptyState";
import { Icon } from "@/features/shell/icons";
import { s } from "@/strings";

const primary = "inline-flex h-9 items-center rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";

/** The workflows the person may see: their own and those shared with them; archived ones on request. */
export function WorkflowsPage() {
  const [archived, setArchived] = useState(false);
  const list = useQuery(workflowsQuery(archived));
  const Glyph = Icon.Workflow;
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={s.places.workflows} />
      <div className="flex flex-wrap items-center gap-3">
        <Link to="/workflows/new" className={primary} data-action="new-workflow">
          {s.workflows.newWorkflow}
        </Link>
        <button type="button" className={secondary} onClick={() => setArchived(!archived)} aria-pressed={archived} data-action="toggle-archived">
          {archived ? s.workflows.showActive : s.workflows.showArchived}
        </button>
      </div>
      {list.isPending && <p className="text-sm text-ink-muted">{s.workflows.loading}</p>}
      {list.isError && (
        <p role="alert" className="text-sm text-danger">
          {s.workflows.unreachable}
        </p>
      )}
      {list.data && list.data.length === 0 && (
        <EmptyState
          icon={<Glyph />}
          title={archived ? s.workflows.archivedTitle : s.workflows.emptyTitle}
          description={archived ? s.workflows.archivedBody : s.workflows.emptyBody}
        />
      )}
      {list.data && list.data.length > 0 && (
        <ul className="flex max-w-3xl flex-col gap-3">
          {list.data.map((w) => (
            <WorkflowRow key={w.id} workflow={w} />
          ))}
        </ul>
      )}
    </div>
  );
}

function WorkflowRow({ workflow }: { workflow: WorkflowSummary }) {
  const client = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [running, setRunning] = useState(false);
  const archive = useMutation({
    mutationFn: () => archiveWorkflow(workflow.id),
    onSuccess: () => client.invalidateQueries({ queryKey: workflowsQueryKey }),
  });
  return (
    <li className="rounded-overlay border border-border bg-surface p-4 shadow-1" data-workflow={workflow.name}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <Link to="/workflows/$workflowID" params={{ workflowID: workflow.id }} className="font-medium text-accent hover:underline">
          {workflow.name}
        </Link>
        <span className="rounded-control border border-border px-2 py-0.5 text-xs text-ink" data-sharing={workflow.sharing}>
          {s.workflows.sharing[workflow.sharing]}
        </span>
      </div>
      {workflow.description && <p className="mt-1 text-sm text-ink-muted">{workflow.description}</p>}
      <p className="mt-1 text-xs text-ink-muted">
        {s.workflows.owner}: {workflow.owner.name} . {s.workflows.version(workflow.currentVersion)}
      </p>
      {!workflow.archived && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <button type="button" className={secondary} onClick={() => setRunning(true)} aria-label={s.runs.runNamed(workflow.name)} data-action="run-workflow">
            {s.runs.run}
          </button>
          {!workflow.canEdit ? null : !confirming ? (
            <button type="button" className={secondary} onClick={() => setConfirming(true)} data-action="archive-workflow">
              {s.workflows.archive}
            </button>
          ) : (
            <span className="flex flex-wrap items-center gap-2" role="group" aria-label={s.workflows.confirmArchive(workflow.name)}>
              <span className="text-sm text-ink">{s.workflows.confirmArchive(workflow.name)}</span>
              <button
                type="button"
                className="h-8 rounded-control bg-danger px-3 text-sm font-medium text-on-danger hover:bg-danger-hover disabled:opacity-60"
                disabled={archive.isPending}
                onClick={() => archive.mutate()}
                data-action="confirm-archive"
              >
                {s.workflows.confirm}
              </button>
              <button type="button" className={secondary} onClick={() => setConfirming(false)}>
                {s.workflows.keep}
              </button>
            </span>
          )}
        </div>
      )}
      {archive.isError && (
        <p role="alert" className="mt-2 text-sm text-danger">
          {archive.error.message}
        </p>
      )}
      {running && <RunDialog workflowID={workflow.id} name={workflow.name} onClose={() => setRunning(false)} />}
    </li>
  );
}
