import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { type Assignment, accessQueryKey, grantOnWorkflow, revokeOnWorkflow, rolesQuery, workflowGrantsQuery } from "@/api/access";
import { groupLabel, groupsQuery } from "@/api/groups";
import { type WorkflowSummary, workflowsQueryKey } from "@/api/workflows";
import { EVERYONE_GROUP, WORKFLOW_GRANT_ROLES } from "@/config";
import { PersonPicker } from "@/features/access/PersonPicker";
import { ConfirmDialog, label, Refusal, select, Tag } from "@/features/access/ui";
import { grantee, useRoleCan } from "@/features/access/words";
import { card, primary, secondary } from "@/features/settings/styles";
import { s } from "@/strings";

/**
 * Who holds a role on this workflow, which is what sharing it means since #203. Anybody who sees the workflow
 * reads the list; its workflow administrators add people or groups as Runner, Editor or Workflow administrator.
 */
export function WorkflowGrantsPanel({ workflow }: { workflow: WorkflowSummary }) {
  const id = useId();
  const client = useQueryClient();
  const grants = useQuery(workflowGrantsQuery(workflow.id));
  const administers = workflow.canAdminister && !workflow.archived;
  const [removing, setRemoving] = useState<Assignment | null>(null);
  // A grant changes what the workflow's list says about it and what people may do with it.
  const refresh = () => Promise.all([client.invalidateQueries({ queryKey: accessQueryKey }), client.invalidateQueries({ queryKey: workflowsQueryKey })]);
  const revoke = useMutation({
    mutationFn: (grant: Assignment) => revokeOnWorkflow(workflow.id, grant.id),
    onSuccess: async () => {
      setRemoving(null);
      await refresh();
    },
  });
  const list = grants.data ?? [];

  return (
    <section className={card} aria-labelledby={`${id}-title`} data-workflow-grants>
      <h2 id={`${id}-title`} className="mb-1 text-lg font-semibold text-ink">
        {s.workflowGrants.title}
      </h2>
      <p className="mb-3 text-sm text-ink-muted">{s.workflowGrants.body}</p>
      <Refusal error={grants.error} />
      {grants.isPending && <p className="text-sm text-ink-muted">{s.workflowGrants.loading}</p>}
      {grants.data && list.length === 0 && <p className="text-sm text-ink-muted">{s.workflowGrants.empty}</p>}
      {list.length > 0 && (
        <ul className="mb-3 flex flex-col gap-2" aria-label={s.workflowGrants.title}>
          {list.map((grant) => (
            <li key={grant.id} className="flex flex-wrap items-center gap-2 text-sm" data-grant={grant.id} data-grantee={grantee(grant)} data-role={grant.role}>
              <span className="min-w-0 break-words text-ink">{grantee(grant)}</span>
              {grant.groupId && <Tag>{s.access.grants.group}</Tag>}
              <span className="text-ink-muted">{grant.roleName}</span>
              {administers && (
                <button
                  type="button"
                  className={`${secondary} ml-auto`}
                  aria-label={s.workflowGrants.removeFrom(grant.roleName, grantee(grant))}
                  onClick={() => {
                    revoke.reset();
                    setRemoving(grant);
                  }}
                  data-action="remove-grant"
                >
                  {s.workflowGrants.remove}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {administers ? <ShareForm workflowID={workflow.id} onShared={refresh} /> : <p className="text-xs text-ink-muted">{s.workflowGrants.readOnly}</p>}
      {removing && (
        <ConfirmDialog
          title={s.workflowGrants.removeTitle(grantee(removing))}
          body={s.workflowGrants.removeBody(grantee(removing), removing.roleName)}
          action={s.workflowGrants.removeGrant}
          pending={revoke.isPending}
          error={revoke.error}
          onConfirm={() => revoke.mutate(removing)}
          onClose={() => setRemoving(null)}
        />
      )}
    </section>
  );
}

function ShareForm({ workflowID, onShared }: { workflowID: string; onShared: () => Promise<unknown> }) {
  const roles = useQuery(rolesQuery);
  const groups = useQuery(groupsQuery());
  const roleCan = useRoleCan();
  const [subject, setSubject] = useState("");
  const [role, setRole] = useState<string>(WORKFLOW_GRANT_ROLES[0]);
  const offered = WORKFLOW_GRANT_ROLES.map((key) => (roles.data ?? []).find((each) => each.key === key)).filter((each) => each !== undefined);
  const chosen = offered.find((each) => each.key === role);
  const allGroups = groups.data ?? [];
  const everyone = allGroups.find((group) => group.builtin === EVERYONE_GROUP);
  const others = allGroups.filter((group) => group !== everyone);
  const share = useMutation({
    mutationFn: () => {
      const [kind, subjectID] = subject.split(":");
      return grantOnWorkflow(workflowID, { role, userId: kind === "user" ? subjectID : undefined, groupId: kind === "group" ? subjectID : undefined });
    },
    onSuccess: async () => {
      setSubject("");
      await onShared();
    },
  });

  return (
    <form
      className="flex flex-col gap-3 border-t border-border pt-3"
      aria-label={s.workflowGrants.share}
      onSubmit={(event) => {
        event.preventDefault();
        if (subject) share.mutate();
      }}
      data-share-form=""
    >
      <PersonPicker
        label={s.workflowGrants.who}
        none={s.access.grants.choose}
        group={s.access.grants.people}
        optionValue={(person) => `user:${person.id}`}
        value={subject}
        onChange={setSubject}
        data-share-who=""
      >
        {everyone && <option value={`group:${everyone.id}`}>{groupLabel(everyone)}</option>}
        {others.length > 0 && (
          <optgroup label={s.access.grants.groups}>
            {others.map((group) => (
              <option key={group.id} value={`group:${group.id}`}>
                {group.name}
              </option>
            ))}
          </optgroup>
        )}
      </PersonPicker>
      <label className={label}>
        {s.workflowGrants.role}
        <select className={select} value={role} onChange={(event) => setRole(event.target.value)} data-share-role="">
          {offered.map((each) => (
            <option key={each.key} value={each.key}>
              {each.name}
            </option>
          ))}
        </select>
      </label>
      {chosen && <p className="text-xs text-ink-muted">{roleCan(chosen)}</p>}
      <div>
        <button type="submit" className={primary} disabled={!subject || share.isPending} data-action="share">
          {s.workflowGrants.share}
        </button>
      </div>
      <Refusal error={share.error} />
    </form>
  );
}
