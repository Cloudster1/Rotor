import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { accessQueryKey, deleteRole, inSentence, permissionsQuery, type Role, rolesQuery, updateRole } from "@/api/access";
import { ADMINISTER_PERMISSION, ADMINISTRATOR_ROLE } from "@/config";
import { primary } from "@/features/settings/styles";
import { s } from "@/strings";
import { NewRoleDialog, RenameRoleDialog, RoleMenu } from "./RoleDialogs";
import { ConfirmDialog, Refusal, Tag, tableFrame, td, th } from "./ui";

/**
 * What each role grants, as a grid: permissions down, roles across, a box at each crossing, saved the moment it
 * is ticked. The built-in roles can be changed but not removed. Ported from Armature's RoleMatrix.
 */
export function RoleMatrix() {
  const client = useQueryClient();
  const roles = useQuery(rolesQuery);
  const permissions = useQuery(permissionsQuery);
  const [adding, setAdding] = useState(false);
  const [renaming, setRenaming] = useState<Role | null>(null);
  const [deleting, setDeleting] = useState<Role | null>(null);
  const [said, setSaid] = useState("");
  const refresh = () => client.invalidateQueries({ queryKey: accessQueryKey });
  const update = useMutation({
    mutationFn: ({ key, permissions }: { key: string; permissions: string[] }) => updateRole(key, { permissions }),
    onSuccess: refresh,
  });
  const remove = useMutation({
    mutationFn: (role: Role) => deleteRole(role.key),
    onSuccess: async (_, role) => {
      setDeleting(null);
      setSaid(s.access.matrix.deleted(role.name));
      await refresh();
    },
  });

  function toggle(role: Role, permission: string, on: boolean) {
    const next = on ? [...role.permissions, permission] : role.permissions.filter((p) => p !== permission);
    setSaid("");
    update.mutate({ key: role.key, permissions: next });
  }

  if (roles.isPending || permissions.isPending) return <p className="text-sm text-ink-muted">{s.access.loading}</p>;

  return (
    <div className="flex flex-col gap-4" data-role-matrix>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="max-w-3xl text-sm text-ink-muted">{s.access.matrix.explain}</p>
        <button type="button" className={primary} onClick={() => setAdding(true)} data-action="new-role">
          {s.access.matrix.newRole}
        </button>
      </div>
      <Refusal error={roles.error ?? permissions.error ?? update.error} />
      <p role="status" className={said ? "text-sm text-ink" : "sr-only"}>
        {said}
      </p>
      <div className={tableFrame}>
        <table className="w-full text-left text-sm">
          <thead className="border-b border-border text-ink-muted">
            <tr>
              <th scope="col" className={th}>
                {s.access.matrix.permission}
              </th>
              {(roles.data ?? []).map((role) => (
                <th key={role.key} scope="col" className={`${th} text-center align-top`} data-role-column={role.key}>
                  <span className="inline-flex flex-col items-center gap-1">
                    <span className="text-xs font-medium whitespace-nowrap text-ink" title={role.description}>
                      {role.name}
                    </span>
                    <span className="inline-flex items-center gap-1">
                      {role.builtin && <Tag>{s.access.matrix.builtIn}</Tag>}
                      <RoleMenu
                        role={role}
                        onRename={() => setRenaming(role)}
                        onDelete={() => {
                          remove.reset();
                          setDeleting(role);
                        }}
                      />
                    </span>
                  </span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {(permissions.data ?? []).map((permission) => (
              <tr key={permission.permission} className="border-b border-border last:border-0" data-permission-row={permission.permission}>
                <td className={td}>
                  <span className="text-ink">{permission.words}</span>
                  <span className="ml-2 font-mono text-2xs text-ink-muted">{permission.permission}</span>
                </td>
                {(roles.data ?? []).map((role) => {
                  // The one tick that cannot come off: without it nobody could put it back.
                  const fixed = role.key === ADMINISTRATOR_ROLE && permission.permission === ADMINISTER_PERMISSION;
                  return (
                    <td key={role.key} className={`${td} text-center`}>
                      <input
                        type="checkbox"
                        className="size-4 accent-accent"
                        aria-label={s.access.matrix.may(role.name, inSentence(permission.words))}
                        checked={role.permissions.includes(permission.permission)}
                        disabled={fixed || update.isPending}
                        onChange={(event) => toggle(role, permission.permission, event.target.checked)}
                        data-permission-cell={`${role.key}:${permission.permission}`}
                      />
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {adding && (
        <NewRoleDialog
          onClose={() => setAdding(false)}
          onAdded={(role) => {
            setAdding(false);
            setSaid(s.access.matrix.added(role.name));
          }}
        />
      )}
      {renaming && (
        <RenameRoleDialog
          role={renaming}
          onClose={() => setRenaming(null)}
          onSaved={(name) => {
            setRenaming(null);
            setSaid(s.access.matrix.saved(name));
          }}
        />
      )}
      {deleting && (
        <ConfirmDialog
          title={s.access.matrix.deleteTitle(deleting.name)}
          body={s.access.matrix.deleteBody(deleting.name, deleting.inUse)}
          action={s.access.matrix.deleteRole}
          pending={remove.isPending}
          error={remove.error}
          onConfirm={() => remove.mutate(deleting)}
          onClose={() => setDeleting(null)}
        />
      )}
    </div>
  );
}
