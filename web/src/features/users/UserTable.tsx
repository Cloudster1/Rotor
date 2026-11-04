import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { accessQueryKey } from "@/api/access";
import { meQuery } from "@/api/auth";
import { groupLabel } from "@/api/groups";
import { type ManagedUser, setUserActive, usersQuery } from "@/api/users";
import { SEARCH_DELAY_MS, USERS_PAGE_SIZE } from "@/config";
import { clampOffset, ConfirmDialog, Pager, Refusal, shownOf, Tag, tableFrame, td, th } from "@/features/access/ui";
import { dangerQuiet, input, secondary } from "@/features/settings/styles";
import { useDebounced } from "@/lib/debounce";
import { s } from "@/strings";

/** What the users page shows: a search and where in its matches the page starts. */
export interface UsersShown {
  q?: string;
  offset?: number;
}

function roleLine(grant: ManagedUser["grants"][number]): string {
  return [grant.roleName, grant.workflowName && s.users.on(grant.workflowName), grant.group && s.users.through(grant.group)].filter(Boolean).join(" ");
}

/**
 * Everybody who has signed in, a page at a time, with their groups and roles: letting somebody go is switching
 * them off here, and switching on brings them back. Ported from Armature's UserTable.
 */
export function UserTable({ q = "", offset = 0, show }: UsersShown & { show: (next: UsersShown, replace?: boolean) => void }) {
  const [words, setWords] = useState(q);
  const settled = useDebounced(words.trim(), SEARCH_DELAY_MS);
  const page = useQuery(usersQuery({ q, offset }));
  const me = useQuery(meQuery);
  const [switchingOff, setSwitchingOff] = useState<ManagedUser | null>(null);
  const [said, setSaid] = useState("");
  const data = page.data;
  const users = data?.users ?? [];

  // The search goes into the address once typing pauses, and starts again from the first page.
  useEffect(() => {
    if (settled !== q) show({ q: settled }, true);
  }, [settled, q, show]);

  // People leave the matches while the page is open; a page past the end steps back.
  useEffect(() => {
    if (data && offset > 0 && offset >= data.total) show({ q, offset: clampOffset(offset, data.total, USERS_PAGE_SIZE) }, true);
  }, [data, offset, q, show]);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <input
          type="search"
          className={`${input} sm:w-72`}
          aria-label={s.users.find}
          placeholder={s.access.people.findPlaceholder}
          value={words}
          onChange={(event) => setWords(event.target.value)}
          data-user-search=""
        />
        {data && (
          <span className="text-sm text-ink-muted tabular-nums" data-users-total={data.total}>
            {shownOf(data.total, data.offset, users.length, s.access.counted.account)}
          </span>
        )}
      </div>
      <Refusal error={page.error} />
      <p role="status" className={said ? "text-sm text-ink" : "sr-only"}>
        {said}
      </p>
      {page.isPending && <p className="text-sm text-ink-muted">{s.access.loading}</p>}
      {data && users.length === 0 && (
        <div className="rounded-overlay border border-border bg-surface px-5 py-6 text-center" data-users-empty>
          <h2 className="text-base font-semibold text-ink">{q ? s.users.noMatch : s.users.empty}</h2>
          <p className="mt-1 text-sm text-ink-muted">{q ? s.users.noMatchDetail : s.users.emptyDetail}</p>
        </div>
      )}
      {users.length > 0 && (
        <div className={tableFrame}>
          <table className="w-full text-left text-sm" data-users>
            <thead className="border-b border-border text-ink-muted">
              <tr>
                {[s.users.name, s.users.groups, s.users.roles, s.users.state].map((heading) => (
                  <th key={heading} scope="col" className={th}>
                    {heading}
                  </th>
                ))}
                <th scope="col" className={th}>
                  <span className="sr-only">{s.access.actions}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {users.map((user) => (
                <UserRow
                  key={user.id}
                  user={user}
                  mine={user.id === me.data?.user.id}
                  onSwitchOff={() => {
                    setSaid("");
                    setSwitchingOff(user);
                  }}
                  onSwitchedOn={() => setSaid(s.users.switchedOn(user.name))}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {data && (
        <Pager label={s.users.pages} total={data.total} offset={data.offset} pageSize={USERS_PAGE_SIZE} onChange={(next) => show({ q, offset: next })} />
      )}
      {switchingOff && (
        <SwitchOffDialog
          user={switchingOff}
          onClose={() => setSwitchingOff(null)}
          onSwitched={() => {
            setSwitchingOff(null);
            setSaid(s.users.switchedOff(switchingOff.name));
          }}
        />
      )}
    </div>
  );
}

function useSwitch(user: ManagedUser, onDone?: () => void) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (isActive: boolean) => setUserActive(user.id, isActive),
    onSuccess: async () => {
      onDone?.();
      await client.invalidateQueries({ queryKey: accessQueryKey });
    },
  });
}

function UserRow({ user, mine, onSwitchOff, onSwitchedOn }: { user: ManagedUser; mine: boolean; onSwitchOff: () => void; onSwitchedOn: () => void }) {
  const switchOn = useSwitch(user, onSwitchedOn);
  return (
    <tr className="border-b border-border last:border-0" data-user={user.email} data-user-active={user.isActive}>
      <td className={`${td} min-w-48`}>
        <span className="text-ink">{user.name}</span>
        {mine && <span className="ml-1 text-xs text-ink-muted">({s.users.you})</span>}
        <span className="block text-xs text-ink-muted">{user.email}</span>
      </td>
      <td className={td}>
        {user.groups.length === 0 ? (
          <span className="text-ink-muted">{s.users.none}</span>
        ) : (
          <ul className="flex flex-wrap gap-1" aria-label={s.users.groups}>
            {user.groups.map((group) => (
              <li key={group.id}>
                <Tag>{groupLabel(group)}</Tag>
              </li>
            ))}
          </ul>
        )}
      </td>
      <td className={td}>
        {user.grants.length === 0 ? (
          <span className="text-ink-muted">{s.users.none}</span>
        ) : (
          <ul className="flex flex-col gap-0.5 text-ink" aria-label={s.users.roles} data-user-roles>
            {user.grants.map((grant) => (
              <li key={`${grant.role}:${grant.workflowId ?? ""}:${grant.group ?? ""}`}>{roleLine(grant)}</li>
            ))}
          </ul>
        )}
      </td>
      <td className={`${td} whitespace-nowrap`}>
        <Tag className={user.isActive ? "" : "text-danger"} data-user-state={user.isActive ? "active" : "off"}>
          {user.isActive ? s.users.active : s.users.off}
        </Tag>
      </td>
      <td className={`${td} text-right`}>
        {user.isActive ? (
          <button type="button" className={dangerQuiet} aria-label={s.users.switchOffNamed(user.name)} onClick={onSwitchOff} data-action="switch-off">
            {s.users.switchOff}
          </button>
        ) : (
          <button
            type="button"
            className={secondary}
            aria-label={s.users.switchOnNamed(user.name)}
            disabled={switchOn.isPending}
            onClick={() => switchOn.mutate(true)}
            data-action="switch-on"
          >
            {s.users.switchOn}
          </button>
        )}
        <div className="mt-1 text-left">
          <Refusal error={switchOn.error} />
        </div>
      </td>
    </tr>
  );
}

// Own account and the last administrator stay on the server's word: its refusal says why, in the dialog.
function SwitchOffDialog({ user, onClose, onSwitched }: { user: ManagedUser; onClose: () => void; onSwitched: () => void }) {
  const switchOff = useSwitch(user, onSwitched);
  return (
    <ConfirmDialog
      title={s.users.switchOffTitle(user.name)}
      body={s.users.switchOffBody(user.name)}
      action={s.users.switchOff}
      pending={switchOff.isPending}
      error={switchOff.error}
      onConfirm={() => switchOff.mutate(false)}
      onClose={onClose}
    />
  );
}
