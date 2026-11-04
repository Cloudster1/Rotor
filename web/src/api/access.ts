import { queryOptions } from "@tanstack/react-query";
import { ROLE_ASSIGNMENT_PAGE_SIZE } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

/** A role as the matrix shows it, a permission with its words, and a grant of a role to somebody somewhere (#203). */
export type Role = components["schemas"]["RoleView"];
export type Permission = components["schemas"]["PermissionView"];
export type Assignment = components["schemas"]["AssignmentView"];
export type AssignmentPage = components["schemas"]["AssignmentPage"];
export type NewRole = components["schemas"]["CreateRoleRequest"];
export type RoleChange = components["schemas"]["UpdateRoleRequest"];
export type GrantInput = components["schemas"]["GrantRequest"];

// Everything access answers sits under one key, since a change to one part changes the counts in the others.
export const accessQueryKey = ["access"] as const;

export const rolesQuery = queryOptions({
  queryKey: [...accessQueryKey, "roles"],
  queryFn: async (): Promise<Role[]> => {
    const { data } = await api.GET("/roles");
    return (data?.roles ?? []) as Role[];
  },
});

export const permissionsQuery = queryOptions({
  queryKey: [...accessQueryKey, "permissions"],
  queryFn: async (): Promise<Permission[]> => {
    const { data } = await api.GET("/permissions");
    return (data?.permissions ?? []) as Permission[];
  },
});

/** What the Roles tab asks for: a search, one role, and where in the matches the page starts. */
export interface AssignmentFilter {
  q: string;
  role: string;
  offset: number;
}

export const assignmentsQuery = ({ q, role, offset }: AssignmentFilter) =>
  queryOptions({
    queryKey: [...accessQueryKey, "assignments", { q, role, offset }],
    queryFn: async (): Promise<AssignmentPage> => {
      const { data } = await api.GET("/role-assignments", {
        params: { query: { q: q || undefined, role: role || undefined, limit: ROLE_ASSIGNMENT_PAGE_SIZE, offset: offset || undefined } },
      });
      return data as AssignmentPage;
    },
    placeholderData: (previous) => previous,
  });

export const workflowGrantsQuery = (workflowID: string) =>
  queryOptions({
    queryKey: [...accessQueryKey, "workflow", workflowID],
    queryFn: async (): Promise<Assignment[]> => {
      const { data } = await api.GET("/workflows/{workflowID}/grants", { params: { path: { workflowID } } });
      return (data?.grants ?? []) as Assignment[];
    },
  });

export async function createRole(body: NewRole): Promise<Role> {
  const { data } = await api.POST("/roles", { body });
  return data?.role as Role;
}

export async function updateRole(roleKey: string, body: RoleChange): Promise<Role> {
  const { data } = await api.PATCH("/roles/{roleKey}", { params: { path: { roleKey } }, body });
  return data?.role as Role;
}

export async function deleteRole(roleKey: string): Promise<void> {
  await api.DELETE("/roles/{roleKey}", { params: { path: { roleKey } } });
}

export async function grantRole(body: GrantInput): Promise<Assignment> {
  const { data } = await api.POST("/role-assignments", { body });
  return data?.assignment as Assignment;
}

export async function revokeRole(assignmentID: string): Promise<void> {
  await api.DELETE("/role-assignments/{assignmentID}", { params: { path: { assignmentID } } });
}

export async function grantOnWorkflow(workflowID: string, body: Omit<GrantInput, "workflowId">): Promise<Assignment> {
  const { data } = await api.POST("/workflows/{workflowID}/grants", { params: { path: { workflowID } }, body });
  return data?.grant as Assignment;
}

export async function revokeOnWorkflow(workflowID: string, grantID: string): Promise<void> {
  await api.DELETE("/workflows/{workflowID}/grants/{grantID}", { params: { path: { workflowID, grantID } } });
}

/** A permission's words as they read inside a sentence, such as "Editor may save new versions of a workflow". */
export function inSentence(words: string): string {
  return words.charAt(0).toLowerCase() + words.slice(1);
}
