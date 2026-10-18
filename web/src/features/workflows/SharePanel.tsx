import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { meQuery } from "@/api/auth";
import { type Sharing, shareWorkflow, type WorkflowSummary, workflowsQueryKey } from "@/api/workflows";
import { s } from "@/strings";

const card = "rounded-overlay border border-border bg-surface p-4 shadow-1";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";

/** Who besides the owner sees a workflow: nobody, groups the owner is in, or everyone. */
export function SharePanel({ workflow }: { workflow: WorkflowSummary }) {
  const id = useId();
  const client = useQueryClient();
  const me = useQuery(meQuery);
  const [sharing, setSharing] = useState<Sharing>(workflow.sharing);
  const [groups, setGroups] = useState<string[]>(workflow.sharedGroups);
  const save = useMutation({
    mutationFn: () => shareWorkflow(workflow.id, sharing, sharing === "groups" ? groups : []),
    onSuccess: () => client.invalidateQueries({ queryKey: workflowsQueryKey }),
  });
  // The person's own groups, and any the workflow is already shared with, so a group they left can be unticked.
  const choices = [...new Set([...(me.data?.groups ?? []), ...workflow.sharedGroups])].sort();
  const options: { value: Sharing; label: string }[] = [
    { value: "private", label: s.workflows.share.private },
    { value: "groups", label: s.workflows.share.groups },
    { value: "everyone", label: s.workflows.share.everyone },
  ];
  return (
    <section className={card} aria-labelledby={`${id}-title`} data-share>
      <h2 id={`${id}-title`} className="mb-1 text-lg font-semibold text-ink">
        {s.workflows.share.title}
      </h2>
      <p className="mb-3 text-sm text-ink-muted">{s.workflows.share.body}</p>
      <fieldset className="mb-3 flex flex-col gap-2">
        <legend className="sr-only">{s.workflows.share.title}</legend>
        {options.map((o) => (
          <label key={o.value} className="flex items-center gap-2 text-sm text-ink">
            <input type="radio" name={`${id}-sharing`} value={o.value} checked={sharing === o.value} onChange={() => setSharing(o.value)} />
            {o.label}
          </label>
        ))}
      </fieldset>
      {sharing === "groups" && (
        <fieldset className="mb-3 flex flex-col gap-2 pl-6" data-share-groups>
          <legend className="sr-only">{s.workflows.share.groups}</legend>
          {choices.length === 0 && <p className="text-sm text-ink-muted">{s.workflows.share.noGroups}</p>}
          {choices.map((g) => (
            <label key={g} className="flex items-center gap-2 text-sm text-ink">
              <input
                type="checkbox"
                value={g}
                checked={groups.includes(g)}
                onChange={(e) => setGroups(e.target.checked ? [...groups, g] : groups.filter((x) => x !== g))}
              />
              {g}
            </label>
          ))}
        </fieldset>
      )}
      <button type="button" className={secondary} disabled={save.isPending} onClick={() => save.mutate()} data-action="save-sharing">
        {s.workflows.share.save}
      </button>
      {save.isSuccess && (
        <p role="status" className="mt-2 text-sm text-ink">
          {s.workflows.share.saved}
        </p>
      )}
      {save.isError && (
        <p role="alert" className="mt-2 text-sm text-danger">
          {save.error.message}
        </p>
      )}
    </section>
  );
}
