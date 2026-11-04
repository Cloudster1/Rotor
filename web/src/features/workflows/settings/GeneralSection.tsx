import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { archiveWorkflow, restoreWorkflow, saveWorkflow, type WorkflowResponse, type WorkflowSummary, workflowsQueryKey } from "@/api/workflows";
import { MAX_WORKFLOW_DESCRIPTION, MAX_WORKFLOW_NAME } from "@/config";
import { input, primary, secondary } from "@/features/settings/styles";
import { SettingsCard, SettingsPage } from "@/features/settings/ui";
import { Refusal } from "@/features/shell/Dialog";
import { s } from "@/strings";
import { withMetadata } from "../metadata";
import { useSettingsWorkflow } from "./shared";

const label = "flex flex-col gap-1 text-sm font-medium text-ink";

/** The workflow's name and description, and archiving it with an Undo. */
export function GeneralPage() {
  const { existing } = useSettingsWorkflow();
  if (!existing) return null;
  return (
    <SettingsPage title={s.workflowSettings.pages.general}>
      <DetailsCard key={existing.version.number} existing={existing} />
      <ArchiveCard workflow={existing.workflow} />
    </SettingsPage>
  );
}

/** Name and description live in the spec's metadata, so saving them saves a version, as the editor does. */
function DetailsCard({ existing }: { existing: WorkflowResponse }) {
  const id = useId();
  const client = useQueryClient();
  const { workflow, version } = existing;
  const [name, setName] = useState(workflow.name);
  const [description, setDescription] = useState(workflow.description);
  const [problem, setProblem] = useState<string | null>(null);
  const editable = workflow.canEdit && !workflow.archived;
  const save = useMutation({
    mutationFn: (yaml: string) => saveWorkflow(workflow.id, yaml, s.workflowSettings.general.comment, workflow.currentVersion),
    onSuccess: () => client.invalidateQueries({ queryKey: workflowsQueryKey }),
  });

  function submit() {
    setProblem(null);
    const next = { name: name.trim(), description: description.trim() };
    if (next.name === "") return setProblem(s.workflowSettings.general.nameNeeded);
    if (next.name === workflow.name && next.description === workflow.description) return setProblem(s.workflowSettings.general.unchanged);
    let yaml: string;
    try {
      yaml = withMetadata(version.specYAML, next);
    } catch {
      return setProblem(s.workflowSettings.general.yamlBroken);
    }
    save.mutate(yaml);
  }

  return (
    <SettingsCard titleId={`${id}-title`} title={s.workflowSettings.general.detailsTitle} description={s.workflowSettings.general.detailsBody} data-details="">
      <form
        className="flex flex-col gap-3"
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        <div className="flex flex-col gap-1">
          <label className={label}>
            {s.workflowSettings.general.name}
            <input
              className={input}
              name="name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              readOnly={!editable}
              maxLength={MAX_WORKFLOW_NAME}
              aria-describedby={`${id}-name-hint`}
              required
            />
          </label>
          <p id={`${id}-name-hint`} className="text-xs text-ink-muted">
            {s.workflowSettings.general.nameHint}
          </p>
        </div>
        <label className={label}>
          {s.workflowSettings.general.description}
          <textarea
            className={`${input} h-24 py-2`}
            name="description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            readOnly={!editable}
            maxLength={MAX_WORKFLOW_DESCRIPTION}
          />
        </label>
        <p className="text-xs text-ink-muted">{s.workflowSettings.general.owner(workflow.owner.name)}</p>
        {editable ? (
          <div>
            <button type="submit" className={primary} disabled={save.isPending} data-action="save-details">
              {save.isPending ? s.workflowSettings.general.saving : s.workflowSettings.general.save}
            </button>
          </div>
        ) : (
          <p className="text-xs text-ink-muted" data-read-only="details">
            {workflow.archived ? s.workflowSettings.general.archivedReadOnly : s.workflowSettings.general.readOnly}
          </p>
        )}
        {problem && (
          <p role="alert" className="text-sm text-danger">
            {problem}
          </p>
        )}
        <Refusal error={save.error} />
        {save.isSuccess && (
          <p role="status" className="text-sm text-ink">
            {s.workflowSettings.general.saved(save.data.version.number)}
          </p>
        )}
      </form>
    </SettingsCard>
  );
}

/** Archiving at once with an Undo beside it, rather than asking first; restoring is the undo, and stays offered. */
function ArchiveCard({ workflow }: { workflow: WorkflowSummary }) {
  const id = useId();
  const client = useQueryClient();
  // What this visit did, so the line under the button says it and offers to take it back.
  const [done, setDone] = useState<"archived" | "restored" | null>(null);
  const refresh = () => client.invalidateQueries({ queryKey: workflowsQueryKey });
  const archive = useMutation({
    mutationFn: () => archiveWorkflow(workflow.id),
    onSuccess: async () => {
      await refresh();
      setDone("archived");
    },
  });
  const restore = useMutation({
    mutationFn: () => restoreWorkflow(workflow.id),
    onSuccess: async () => {
      await refresh();
      setDone("restored");
    },
  });
  const pending = archive.isPending || restore.isPending;
  const general = s.workflowSettings.general;

  return (
    <SettingsCard titleId={`${id}-title`} title={general.archiveTitle} description={general.archiveBody} data-archive="">
      <div className="flex flex-col gap-3">
        {workflow.archived && done !== "archived" && <p className="text-sm text-ink">{general.isArchived}</p>}
        {!workflow.canAdminister && (
          <p className="text-xs text-ink-muted" data-read-only="archive">
            {general.archiveReadOnly}
          </p>
        )}
        {workflow.canAdminister && !workflow.archived && (
          <div>
            <button type="button" className={secondary} disabled={pending} onClick={() => archive.mutate()} data-action="archive-workflow">
              {archive.isPending ? general.archiving : general.archive}
            </button>
          </div>
        )}
        {workflow.canAdminister && workflow.archived && done !== "archived" && (
          <div>
            <button type="button" className={secondary} disabled={pending} onClick={() => restore.mutate()} data-action="restore-workflow">
              {restore.isPending ? general.restoring : general.restore}
            </button>
          </div>
        )}
        {/* Present from the start, so a screen reader hears what changes in it. */}
        <div role="status" className="flex flex-wrap items-center gap-2 text-sm text-ink" data-archive-status={done ?? ""}>
          {done === "archived" && workflow.archived && (
            <>
              <span>{general.archived(workflow.name)}</span>
              <button type="button" className={secondary} disabled={pending} onClick={() => restore.mutate()} data-action="undo-archive">
                {general.undo}
              </button>
            </>
          )}
          {done === "restored" && !workflow.archived && <span>{general.restored(workflow.name)}</span>}
        </div>
        <Refusal error={archive.error ?? restore.error} />
      </div>
    </SettingsCard>
  );
}
