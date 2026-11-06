import { useMutation, useQuery, type UseQueryResult } from "@tanstack/react-query";
import { type ReactNode, useId, useState } from "react";
import { type Assignment, type GrantInput, rolesQuery } from "@/api/access";
import { groupLabel, groupsQuery } from "@/api/groups";
import { EVERYONE_GROUP } from "@/config";
import { PersonPicker } from "@/features/access/PersonPicker";
import { ConfirmDialog, label, Refusal, select, Tag } from "@/features/access/ui";
import { grantee, useRoleCan } from "@/features/access/words";
import { card, primary, secondary } from "@/features/settings/styles";
import { s } from "@/strings";

/** The words of one kind of thing's sharing; a workflow's and a skill's say the same things of different things. */
export interface GrantWords {
  title: string;
  body: string;
  readOnly: string;
  loading: string;
  empty: string;
  who: string;
  role: string;
  share: string;
  remove: string;
  removeFrom: (role: string, who: string) => string;
  removeTitle: (who: string) => string;
  removeBody: (who: string, role: string) => string;
  removeGrant: string;
}

/** Who holds a role on one workflow or skill (#203, #223): everybody who sees it reads the list, its administrators change it. */
export function GrantsPanel({
  grants,
  roles,
  administers,
  words,
  grant,
  revoke,
  refresh,
  note,
  ...data
}: {
  grants: UseQueryResult<Assignment[]>;
  roles: readonly string[];
  administers: boolean;
  words: GrantWords;
  grant: (body: Omit<GrantInput, "workflowId">) => Promise<Assignment>;
  revoke: (grantID: string) => Promise<void>;
  refresh: () => Promise<unknown>;
  note?: ReactNode;
} & Record<`data-${string}`, string>) {
  const id = useId();
  const [removing, setRemoving] = useState<Assignment | null>(null);
  const take = useMutation({
    mutationFn: (each: Assignment) => revoke(each.id),
    onSuccess: async () => {
      setRemoving(null);
      await refresh();
    },
  });
  const list = grants.data ?? [];

  return (
    <section className={card} aria-labelledby={`${id}-title`} {...data}>
      <h2 id={`${id}-title`} className="mb-1 text-lg font-semibold text-ink">
        {words.title}
      </h2>
      <p className="mb-3 text-sm text-ink-muted">{words.body}</p>
      {note}
      <Refusal error={grants.error} />
      {grants.isPending && <p className="text-sm text-ink-muted">{words.loading}</p>}
      {grants.data && list.length === 0 && <p className="text-sm text-ink-muted">{words.empty}</p>}
      {list.length > 0 && (
        <ul className="mb-3 flex flex-col gap-2" aria-label={words.title}>
          {list.map((each) => (
            <li key={each.id} className="flex flex-wrap items-center gap-2 text-sm" data-grant={each.id} data-grantee={grantee(each)} data-role={each.role}>
              <span className="min-w-0 break-words text-ink">{grantee(each)}</span>
              {each.groupId && <Tag>{s.access.grants.group}</Tag>}
              <span className="text-ink-muted">{each.roleName}</span>
              {administers && (
                <button
                  type="button"
                  className={`${secondary} ml-auto`}
                  aria-label={words.removeFrom(each.roleName, grantee(each))}
                  onClick={() => {
                    take.reset();
                    setRemoving(each);
                  }}
                  data-action="remove-grant"
                >
                  {words.remove}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {administers ? <ShareForm roles={roles} words={words} grant={grant} onShared={refresh} /> : <p className="text-xs text-ink-muted">{words.readOnly}</p>}
      {removing && (
        <ConfirmDialog
          title={words.removeTitle(grantee(removing))}
          body={words.removeBody(grantee(removing), removing.roleName)}
          action={words.removeGrant}
          pending={take.isPending}
          error={take.error}
          onConfirm={() => take.mutate(removing)}
          onClose={() => setRemoving(null)}
        />
      )}
    </section>
  );
}

function ShareForm({
  roles: keys,
  words,
  grant,
  onShared,
}: {
  roles: readonly string[];
  words: GrantWords;
  grant: (body: Omit<GrantInput, "workflowId">) => Promise<Assignment>;
  onShared: () => Promise<unknown>;
}) {
  const roles = useQuery(rolesQuery);
  const groups = useQuery(groupsQuery());
  const roleCan = useRoleCan();
  const [subject, setSubject] = useState("");
  const [role, setRole] = useState<string>(keys[0] ?? "");
  const offered = keys.map((key) => (roles.data ?? []).find((each) => each.key === key)).filter((each) => each !== undefined);
  const chosen = offered.find((each) => each.key === role);
  const allGroups = groups.data ?? [];
  const everyone = allGroups.find((group) => group.builtin === EVERYONE_GROUP);
  const others = allGroups.filter((group) => group !== everyone);
  const share = useMutation({
    mutationFn: () => {
      const [kind, subjectID] = subject.split(":");
      return grant({ role, userId: kind === "user" ? subjectID : undefined, groupId: kind === "group" ? subjectID : undefined });
    },
    onSuccess: async () => {
      setSubject("");
      await onShared();
    },
  });

  return (
    <form
      className="flex flex-col gap-3 border-t border-border pt-3"
      aria-label={words.share}
      onSubmit={(event) => {
        event.preventDefault();
        if (subject) share.mutate();
      }}
      data-share-form=""
    >
      <PersonPicker
        label={words.who}
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
        {words.role}
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
          {words.share}
        </button>
      </div>
      <Refusal error={share.error} />
    </form>
  );
}
