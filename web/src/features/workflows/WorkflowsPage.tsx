import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { myTriggersQuery } from "@/api/triggers";
import { archiveWorkflow, type WorkflowSummary, workflowsQuery, workflowsQueryKey } from "@/api/workflows";
import { RunDialog } from "@/features/runs/RunDialog";
import { ConfirmDialog } from "@/features/shell/Dialog";
import { EmptyState, PageHeader } from "@/features/shell/EmptyState";
import { Icon } from "@/features/shell/icons";
import { s } from "@/strings";

const primary = "inline-flex h-9 items-center rounded-control bg-accent px-4 text-sm font-medium text-on-accent hover:bg-accent-hover";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";

/** The workflows the person may see: those their grants, their groups' or Everyone's give them; archived ones on request. */
export function WorkflowsPage() {
  const [archived, setArchived] = useState(false);
  const list = useQuery(workflowsQuery(archived));
  // A trigger Rotor turned off is flagged on its workflow, since nobody is told otherwise.
  const mine = useQuery(myTriggersQuery);
  const turnedOff = new Set((mine.data ?? []).filter((t) => t.disabled).map((t) => t.workflow.id));
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
            <WorkflowRow key={w.id} workflow={w} triggerOff={turnedOff.has(w.id)} />
          ))}
        </ul>
      )}
    </div>
  );
}

function WorkflowRow({ workflow, triggerOff }: { workflow: WorkflowSummary; triggerOff: boolean }) {
  const client = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [running, setRunning] = useState(false);
  const archive = useMutation({
    mutationFn: () => archiveWorkflow(workflow.id),
    onSuccess: async () => {
      setConfirming(false);
      await client.invalidateQueries({ queryKey: workflowsQueryKey });
    },
  });
  return (
    <li className="rounded-overlay border border-border bg-surface p-4 shadow-1" data-workflow={workflow.name}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <Link to="/workflows/$workflowID" params={{ workflowID: workflow.id }} className="font-medium text-accent hover:underline">
          {workflow.name}
        </Link>
        <span className="rounded-control border border-border px-2 py-0.5 text-xs text-ink" data-shared-with={workflow.sharedWith}>
          {s.workflowList.sharedWith(workflow.sharedWith)}
        </span>
      </div>
      {workflow.description && <p className="mt-1 text-sm text-ink-muted">{workflow.description}</p>}
      {triggerOff && (
        <p className="mt-2 rounded-control border border-warning bg-warning-subtle px-3 py-1 text-sm text-ink" data-trigger-off>
          {s.triggers.flagNamed(workflow.name)}
        </p>
      )}
      <p className="mt-1 text-xs text-ink-muted">
        {s.workflows.owner}: {workflow.owner.name} . {s.workflows.version(workflow.currentVersion)} .{" "}
        <span data-yours={yours(workflow)}>{s.workflowList.yours[yours(workflow)]}</span>
      </p>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        {workflow.canRun && !workflow.archived && (
          <button type="button" className={secondary} onClick={() => setRunning(true)} aria-label={s.runs.runNamed(workflow.name)} data-action="run-workflow">
            {s.runs.run}
          </button>
        )}
        <Link
          to="/workflows/$workflowID/settings/general"
          params={{ workflowID: workflow.id }}
          className={`${secondary} inline-flex items-center`}
          aria-label={s.workflowSettings.linkNamed(workflow.name)}
          data-action="workflow-settings"
        >
          {s.workflowSettings.link}
        </Link>
        {workflow.canAdminister && !workflow.archived && (
          <button type="button" className={secondary} onClick={() => setConfirming(true)} data-action="archive-workflow">
            {s.workflows.archive}
          </button>
        )}
      </div>
      {confirming && (
        <ConfirmDialog
          title={s.workflowList.archiveTitle(workflow.name)}
          body={s.workflowList.archiveBody}
          action={s.workflowList.archiveAction}
          pending={archive.isPending}
          error={archive.error}
          onConfirm={() => archive.mutate()}
          onClose={() => {
            setConfirming(false);
            archive.reset();
          }}
        />
      )}
      {running && <RunDialog workflowID={workflow.id} name={workflow.name} onClose={() => setRunning(false)} />}
    </li>
  );
}

/** What the caller's grants let them do with a workflow, in one word, the most first. */
function yours(workflow: WorkflowSummary): keyof typeof s.workflowList.yours {
  if (workflow.canAdminister) return "administer";
  if (workflow.canEdit) return "edit";
  if (workflow.canRun) return "run";
  return "read";
}
