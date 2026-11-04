import { useQuery } from "@tanstack/react-query";
import { type Assignment, inSentence, permissionsQuery, type Role } from "@/api/access";
import { groupLabel } from "@/api/groups";
import { s } from "@/strings";

/** Who a grant goes to, as a row names them. */
export function grantee(assignment: Assignment): string {
  return assignment.groupId ? groupLabel(assignment) : (assignment.userName ?? "");
}

/** What a role lets its holders do, as a sentence under a form that grants it. */
export function useRoleCan(): (role: Role) => string {
  const permissions = useQuery(permissionsQuery);
  const words = new Map((permissions.data ?? []).map((each) => [each.permission, each.words]));
  return (role) => s.access.grants.can(role.name, role.permissions.map((p) => inSentence(words.get(p) ?? p)).join(", "));
}
