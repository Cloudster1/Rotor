import { queryOptions } from "@tanstack/react-query";
import { EVERYONE_GROUP } from "@/config";
import { s } from "@/strings";
import { accessQueryKey } from "./access";
import { api } from "./client";
import type { components } from "./schema";

/** A group: Everyone, one the identity provider fills, or one made by hand (#203); and a person to pick. */
export type Group = components["schemas"]["GroupView"];
export type Person = components["schemas"]["PersonView"];
export type NewGroup = components["schemas"]["CreateGroupRequest"];

export const groupsQuery = (enabled = true) =>
  queryOptions({
    queryKey: [...accessQueryKey, "groups"],
    queryFn: async (): Promise<Group[]> => {
      const { data } = await api.GET("/groups");
      return (data?.groups ?? []) as Group[];
    },
    enabled,
  });

export const groupQuery = (groupID: string) =>
  queryOptions({
    queryKey: [...accessQueryKey, "groups", groupID],
    queryFn: async (): Promise<Group> => {
      const { data } = await api.GET("/groups/{groupID}", { params: { path: { groupID } } });
      return data?.group as Group;
    },
  });

/** The people let in whose name or address matches, for a picker; an empty search asks for nobody. */
export const peopleQuery = (q: string) =>
  queryOptions({
    queryKey: [...accessQueryKey, "people", q],
    queryFn: async (): Promise<Person[]> => {
      const { data } = await api.GET("/people", { params: { query: { q } } });
      return (data?.people ?? []) as Person[];
    },
    enabled: q !== "",
  });

export async function createGroup(body: NewGroup): Promise<Group> {
  const { data } = await api.POST("/groups", { body });
  return data?.group as Group;
}

export async function deleteGroup(groupID: string): Promise<void> {
  await api.DELETE("/groups/{groupID}", { params: { path: { groupID } } });
}

export async function addToGroup(groupID: string, userId: string): Promise<Group> {
  const { data } = await api.POST("/groups/{groupID}/members", { params: { path: { groupID } }, body: { userId } });
  return data?.group as Group;
}

export async function removeFromGroup(groupID: string, userID: string): Promise<Group> {
  const { data } = await api.DELETE("/groups/{groupID}/members/{userID}", { params: { path: { groupID, userID } } });
  return data?.group as Group;
}

/** How a group reads: Everyone in the interface's words, any other by its name. */
export function groupLabel(group: { name?: string; builtin?: string; groupName?: string; groupBuiltin?: string }): string {
  const builtin = group.builtin ?? group.groupBuiltin;
  return builtin === EVERYONE_GROUP ? s.access.groups.everyone : (group.name ?? group.groupName ?? "");
}
