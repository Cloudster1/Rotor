import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { accessQueryKey } from "@/api/access";
import { addToGroup, createGroup, deleteGroup, type Group, groupLabel, groupQuery, groupsQuery, removeFromGroup } from "@/api/groups";
import { card, dangerQuiet, input, primary, secondary } from "@/features/settings/styles";
import { s } from "@/strings";
import { PersonPicker } from "./PersonPicker";
import { ConfirmDialog, label, Refusal, Tag } from "./ui";

const counts = (group: Group) => `${s.access.groups.people(group.memberCount)} . ${s.access.groups.roles(group.roleCount)}`;

/**
 * Groups, and who is in them: Everyone first, then the identity provider's, then the hand-made ones. A group the
 * provider fills is shown but not edited, since a change here would last only until the next sign-in. From Armature.
 */
export function GroupList() {
  const groups = useQuery(groupsQuery());
  const list = groups.data ?? [];
  return (
    <div className="flex flex-col gap-4">
      <NewGroup />
      <Refusal error={groups.error} />
      {groups.isPending && <p className="text-sm text-ink-muted">{s.access.loading}</p>}
      {groups.data && list.every((group) => group.builtin) && (
        <div className="rounded-overlay border border-border bg-surface px-5 py-6 text-center" data-groups-empty>
          <h2 className="text-base font-semibold text-ink">{s.access.groups.empty}</h2>
          <p className="mt-1 text-sm text-ink-muted">{s.access.groups.emptyDetail}</p>
        </div>
      )}
      <ul className="flex flex-col gap-3">
        {list.map((group) => (
          <li key={group.id}>{group.builtin ? <BuiltinGroupCard group={group} /> : <GroupCard group={group} />}</li>
        ))}
      </ul>
    </div>
  );
}

/** Everyone, whose members are everybody: listed by count, since it holds them all, and never edited here. */
function BuiltinGroupCard({ group }: { group: Group }) {
  return (
    <section className={card} aria-label={groupLabel(group)} data-group={groupLabel(group)} data-group-builtin={group.builtin}>
      <h2 className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-semibold text-ink">
        <span className="break-words">{groupLabel(group)}</span>
        <Tag>{s.access.groups.builtin}</Tag>
      </h2>
      <p className="mt-0.5 text-xs text-ink-muted">{counts(group)}</p>
      <p className="mt-2 text-sm text-ink-muted">{s.access.groups.everyoneDetail}</p>
    </section>
  );
}

function GroupCard({ group }: { group: Group }) {
  const client = useQueryClient();
  const detail = useQuery(groupQuery(group.id));
  const [picked, setPicked] = useState("");
  const [confirming, setConfirming] = useState<{ kind: "group" } | { kind: "member"; userId: string; name: string } | null>(null);
  const refresh = () => client.invalidateQueries({ queryKey: accessQueryKey });
  const add = useMutation({ mutationFn: (userId: string) => addToGroup(group.id, userId), onSuccess: refresh });
  const remove = useMutation({
    mutationFn: (userId: string) => removeFromGroup(group.id, userId),
    onSuccess: async () => {
      setConfirming(null);
      await refresh();
    },
  });
  const drop = useMutation({ mutationFn: () => deleteGroup(group.id), onSuccess: refresh });
  const members = detail.data?.members ?? [];
  const fromProvider = group.source === "oidc";

  return (
    <section className={card} aria-label={group.name} data-group={group.name} data-group-source={group.source}>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm font-semibold text-ink">
            <span className="break-words">{group.name}</span>
            {fromProvider && <Tag>{s.access.groups.fromProvider}</Tag>}
          </h2>
          <p className="mt-0.5 text-xs text-ink-muted">
            {counts(group)}
            {group.externalRef && s.access.groups.matches(group.externalRef)}
          </p>
        </div>
        <button
          type="button"
          className={`${dangerQuiet} shrink-0`}
          onClick={() => {
            drop.reset();
            setConfirming({ kind: "group" });
          }}
          data-action="delete-group"
        >
          {s.access.delete}
        </button>
      </div>

      <ul className="mt-2 flex flex-col gap-1 text-sm" aria-label={group.name}>
        {members.map((member) => (
          <li key={member.userId} className="flex items-center gap-2" data-member={member.name}>
            <span className="min-w-0 truncate text-ink">{member.name}</span>
            <span className="min-w-0 truncate text-xs text-ink-muted">{member.email}</span>
            {!fromProvider && (
              <button
                type="button"
                className={`${secondary} ml-auto`}
                aria-label={s.access.groups.removeFrom(member.name, group.name)}
                onClick={() => {
                  remove.reset();
                  setConfirming({ kind: "member", userId: member.userId, name: member.name });
                }}
                data-action="remove-member"
              >
                {s.access.remove}
              </button>
            )}
          </li>
        ))}
        {detail.data && members.length === 0 && (
          <li className="text-sm text-ink-muted">{fromProvider ? s.access.groups.nobodyFromProvider : s.access.groups.nobody}</li>
        )}
      </ul>

      {!fromProvider && (
        <form
          className="mt-3 flex flex-wrap items-end gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            if (!picked) return;
            add.mutate(picked);
            setPicked("");
          }}
        >
          <div className="min-w-56 flex-1">
            <PersonPicker
              label={s.access.groups.addTo(group.name)}
              none={s.access.groups.addSomebody}
              exclude={members.map((member) => member.userId)}
              value={picked}
              onChange={setPicked}
              data-group-add=""
            />
          </div>
          <button type="submit" className={primary} disabled={!picked || add.isPending} data-action="add-member">
            {s.access.add}
          </button>
        </form>
      )}
      <div className="mt-2">
        <Refusal error={add.error ?? drop.error} />
      </div>

      {confirming?.kind === "group" && (
        <ConfirmDialog
          title={s.access.groups.deleteTitle(group.name)}
          body={s.access.groups.deleteBody(group.name)}
          action={s.access.groups.deleteGroup}
          pending={drop.isPending}
          error={drop.error}
          onConfirm={() => drop.mutate()}
          onClose={() => setConfirming(null)}
        />
      )}
      {confirming?.kind === "member" && (
        <ConfirmDialog
          title={s.access.groups.removeTitle(confirming.name)}
          body={s.access.groups.removeBody(confirming.name, group.name)}
          action={s.access.groups.removeMember}
          pending={remove.isPending}
          error={remove.error}
          onConfirm={() => remove.mutate(confirming.userId)}
          onClose={() => setConfirming(null)}
        />
      )}
    </section>
  );
}

function NewGroup() {
  const client = useQueryClient();
  const [name, setName] = useState("");
  const [ref, setRef] = useState("");
  const create = useMutation({
    mutationFn: () => createGroup({ name: name.trim(), externalRef: ref.trim() || undefined }),
    onSuccess: async () => {
      setName("");
      setRef("");
      await client.invalidateQueries({ queryKey: accessQueryKey });
    },
  });
  // The hint sits under the whole row rather than under its field, so the two inputs and the button line up.
  return (
    <form
      className={`${card} flex flex-col gap-3`}
      aria-label={s.access.groups.newGroup}
      onSubmit={(event) => {
        event.preventDefault();
        if (name.trim()) create.mutate();
      }}
      data-new-group=""
    >
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
        <label className={label}>
          {s.access.groups.newGroup}
          <input className={input} value={name} placeholder={s.access.groups.newGroupPlaceholder} onChange={(event) => setName(event.target.value)} />
        </label>
        <label className={label}>
          {s.access.groups.claim}
          <input className={input} value={ref} placeholder={s.access.optional} onChange={(event) => setRef(event.target.value)} />
        </label>
        <button type="submit" className={primary} disabled={!name.trim() || create.isPending} data-action="create-group">
          {s.access.groups.create}
        </button>
      </div>
      <p className="text-xs text-ink-muted">{s.access.groups.claimHint}</p>
      <Refusal error={create.error} />
    </form>
  );
}
