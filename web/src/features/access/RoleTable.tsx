import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { type Assignment, accessQueryKey, assignmentsQuery, revokeRole, rolesQuery } from "@/api/access";
import { ROLE_ASSIGNMENT_PAGE_SIZE, SEARCH_DELAY_MS } from "@/config";
import { input, secondary } from "@/features/settings/styles";
import { useDebounced } from "@/lib/debounce";
import { s } from "@/strings";
import { GrantForm } from "./GrantForm";
import { ConfirmDialog, clampOffset, Pager, Refusal, select, shownOf, Tag, tableFrame, td, th } from "./ui";
import { grantee } from "./words";

/**
 * Who holds what: a role, where it applies and to whom, a page at a time with a search and a role filter.
 * Ported from Armature's RoleTable, where reading it answers "why can this person do that".
 */
export function RoleTable() {
  const client = useQueryClient();
  // Held here rather than in the address, like Armature's: the page is the tab's own.
  const [q, setQ] = useState("");
  const [role, setRole] = useState("");
  const [offset, setOffset] = useState(0);
  const [revoking, setRevoking] = useState<Assignment | null>(null);
  const settled = useDebounced(q.trim(), SEARCH_DELAY_MS);
  const page = useQuery(assignmentsQuery({ q: settled, role, offset }));
  const roles = useQuery(rolesQuery);
  const revoke = useMutation({
    mutationFn: (assignment: Assignment) => revokeRole(assignment.id),
    onSuccess: async () => {
      setRevoking(null);
      await client.invalidateQueries({ queryKey: accessQueryKey });
    },
  });

  const data = page.data;
  const assignments = data?.assignments ?? [];
  const narrowed = Boolean(settled || role);

  // Grants leave the list while it is open; a page past the end steps back.
  useEffect(() => {
    if (data && offset > 0 && offset >= data.total) setOffset(clampOffset(offset, data.total, ROLE_ASSIGNMENT_PAGE_SIZE));
  }, [data, offset]);

  return (
    <div className="flex flex-col gap-4">
      <GrantForm />
      <Refusal error={page.error} />
      <div className="flex flex-wrap items-center gap-3">
        <input
          type="search"
          className={`${input} sm:w-72`}
          aria-label={s.access.grants.find}
          placeholder={s.access.grants.findPlaceholder}
          value={q}
          onChange={(event) => {
            setQ(event.target.value);
            setOffset(0);
          }}
          data-assignment-search=""
        />
        <select
          className={`${select} sm:w-56`}
          aria-label={s.access.grants.onlyRole}
          value={role}
          onChange={(event) => {
            setRole(event.target.value);
            setOffset(0);
          }}
          data-assignment-role=""
        >
          <option value="">{s.access.grants.everyRole}</option>
          {(roles.data ?? []).map((each) => (
            <option key={each.key} value={each.key}>
              {each.name}
            </option>
          ))}
        </select>
        {data && (
          <span className="text-sm text-ink-muted tabular-nums" data-assignments-shown>
            {shownOf(data.total, data.offset, assignments.length, s.access.counted.grant)}
          </span>
        )}
      </div>

      {page.isPending && <p className="text-sm text-ink-muted">{s.access.loading}</p>}
      {data && assignments.length === 0 && (
        <div className="rounded-overlay border border-border bg-surface px-5 py-6 text-center" data-assignments-empty>
          <h2 className="text-base font-semibold text-ink">{narrowed ? s.access.grants.noMatch : s.access.grants.empty}</h2>
          <p className="mt-1 text-sm text-ink-muted">{narrowed ? s.access.grants.noMatchDetail : s.access.grants.emptyDetail}</p>
        </div>
      )}
      {assignments.length > 0 && (
        <div className={tableFrame}>
          <table className="w-full text-left text-sm" data-role-assignments>
            <thead className="border-b border-border text-ink-muted">
              <tr>
                <th scope="col" className={th}>
                  {s.access.grants.who}
                </th>
                <th scope="col" className={th}>
                  {s.access.grants.role}
                </th>
                <th scope="col" className={th}>
                  {s.access.grants.where}
                </th>
                <th scope="col" className={th}>
                  <span className="sr-only">{s.access.actions}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {assignments.map((assignment) => (
                <tr
                  key={assignment.id}
                  className="border-b border-border last:border-0"
                  data-assignment={assignment.id}
                  data-grantee={grantee(assignment)}
                  data-role={assignment.role}
                >
                  <td className={`${td} w-full min-w-48`}>
                    <span className="text-ink">{grantee(assignment)}</span>
                    {assignment.groupId ? (
                      <Tag className="ml-2">{s.access.grants.group}</Tag>
                    ) : (
                      assignment.userEmail && <span className="block text-xs text-ink-muted">{assignment.userEmail}</span>
                    )}
                  </td>
                  <td className={`${td} whitespace-nowrap text-ink`}>{assignment.roleName}</td>
                  <td className={`${td} whitespace-nowrap text-ink-muted`}>{assignment.workflowName || s.access.grants.everyWorkflow}</td>
                  <td className={`${td} text-right`}>
                    <button
                      type="button"
                      className={secondary}
                      aria-label={s.access.grants.revokeFrom(assignment.roleName, grantee(assignment))}
                      onClick={() => {
                        revoke.reset();
                        setRevoking(assignment);
                      }}
                      data-action="revoke"
                    >
                      {s.access.grants.revoke}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {data && <Pager label={s.access.grants.pages} total={data.total} offset={data.offset} pageSize={ROLE_ASSIGNMENT_PAGE_SIZE} onChange={setOffset} />}
      {revoking && (
        <ConfirmDialog
          title={s.access.grants.revokeTitle}
          body={s.access.grants.revokeBody(revoking.roleName)}
          action={s.access.grants.revokeRole}
          pending={revoke.isPending}
          error={revoke.error}
          onConfirm={() => revoke.mutate(revoking)}
          onClose={() => setRevoking(null)}
        />
      )}
    </div>
  );
}
