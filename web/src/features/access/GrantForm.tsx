import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { accessQueryKey, grantRole, rolesQuery } from "@/api/access";
import { groupLabel, groupsQuery } from "@/api/groups";
import { workflowsQuery } from "@/api/workflows";
import { EVERYONE_GROUP, WORKFLOW_GRANT_ROLES } from "@/config";
import { card, primary } from "@/features/settings/styles";
import { s } from "@/strings";
import { PersonPicker } from "./PersonPicker";
import { label, Refusal, select } from "./ui";
import { useRoleCan } from "./words";

/** Gives a role to a person or a group, over one workflow or the whole installation. Ported from Armature's. */
export function GrantForm() {
  const id = useId();
  const client = useQueryClient();
  const roles = useQuery(rolesQuery);
  const groups = useQuery(groupsQuery());
  const workflows = useQuery(workflowsQuery(false));
  const roleCan = useRoleCan();
  const [subject, setSubject] = useState("");
  const [role, setRole] = useState("");
  const [where, setWhere] = useState("");

  const list = roles.data ?? [];
  const roleKey = role || (list.find((each) => each.key === WORKFLOW_GRANT_ROLES[0]) ?? list[0])?.key || "";
  const chosen = list.find((each) => each.key === roleKey);
  // Administering Rotor over one workflow would read as a smaller thing than it is, so the server refuses it and the form does not offer it.
  const scopeIsFixed = chosen?.installationWideOnly ?? false;
  const allGroups = groups.data ?? [];
  const everyone = allGroups.find((group) => group.builtin === EVERYONE_GROUP);
  const others = allGroups.filter((group) => group !== everyone);

  const grant = useMutation({
    mutationFn: () => {
      const [kind, subjectID] = subject.split(":");
      return grantRole({
        role: roleKey,
        workflowId: scopeIsFixed ? undefined : where || undefined,
        userId: kind === "user" ? subjectID : undefined,
        groupId: kind === "group" ? subjectID : undefined,
      });
    },
    onSuccess: async () => {
      setSubject("");
      await client.invalidateQueries({ queryKey: accessQueryKey });
    },
  });

  return (
    <form
      className={`${card} flex flex-col gap-3`}
      aria-label={s.access.grants.grant}
      onSubmit={(event) => {
        event.preventDefault();
        if (subject && roleKey) grant.mutate();
      }}
      data-grant-form=""
    >
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)_minmax(0,1fr)_auto] lg:items-end">
        <PersonPicker
          label={s.access.grants.whoLabel}
          none={s.access.grants.choose}
          group={s.access.grants.people}
          optionValue={(person) => `user:${person.id}`}
          value={subject}
          onChange={setSubject}
          data-grant-who=""
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
          {s.access.grants.role}
          <select className={select} value={roleKey} onChange={(event) => setRole(event.target.value)} data-grant-role="">
            {list.map((each) => (
              <option key={each.key} value={each.key}>
                {each.name}
              </option>
            ))}
          </select>
        </label>
        <label className={label} htmlFor={`${id}-where`}>
          {s.access.grants.whereLabel}
          <select
            id={`${id}-where`}
            className={`${select} disabled:opacity-60`}
            value={scopeIsFixed ? "" : where}
            disabled={scopeIsFixed}
            onChange={(event) => setWhere(event.target.value)}
            data-grant-where=""
          >
            <option value="">{s.access.grants.everyWorkflow}</option>
            {(workflows.data ?? []).map((workflow) => (
              <option key={workflow.id} value={workflow.id}>
                {workflow.name}
              </option>
            ))}
          </select>
        </label>
        <button type="submit" className={primary} disabled={!subject || !roleKey || grant.isPending} data-action="grant">
          {s.access.grants.grant}
        </button>
      </div>
      {chosen && <p className="text-xs text-ink-muted">{roleCan(chosen)}</p>}
      <Refusal error={grant.error} />
    </form>
  );
}
