import { queryOptions } from "@tanstack/react-query";
import { USERS_PAGE_SIZE } from "@/config";
import { accessQueryKey } from "./access";
import { api } from "./client";
import type { components } from "./schema";

/** A person as Admin's Users page shows them: their groups, their roles and whether they may sign in (#203). */
export type ManagedUser = components["schemas"]["UserView"];
export type UserPage = components["schemas"]["UserPage"];

export const usersQuery = ({ q, offset }: { q: string; offset: number }) =>
  queryOptions({
    queryKey: [...accessQueryKey, "users", { q, offset }],
    queryFn: async (): Promise<UserPage> => {
      const { data } = await api.GET("/users", { params: { query: { q: q || undefined, limit: USERS_PAGE_SIZE, offset: offset || undefined } } });
      return data as UserPage;
    },
    placeholderData: (previous) => previous,
  });

/** Switches a person off, which ends their sessions and connections, or on again. */
export async function setUserActive(userID: string, isActive: boolean): Promise<ManagedUser> {
  const { data } = await api.PATCH("/users/{userID}", { params: { path: { userID } }, body: { isActive } });
  return data?.user as ManagedUser;
}

/** Somebody who signed in through the provider and waits to be let in (#204). */
export type JoinRequest = components["schemas"]["JoinRequestView"];

export const joinRequestsQuery = queryOptions({
  queryKey: [...accessQueryKey, "users", "requests"],
  queryFn: async (): Promise<JoinRequest[]> => {
    const { data } = await api.GET("/users/requests");
    return (data?.requests ?? []) as JoinRequest[];
  },
});

/** Lets a waiting person in as a member; they sign in from their next attempt. */
export async function admitJoinRequest(userID: string): Promise<ManagedUser> {
  const { data } = await api.POST("/users/requests/{userID}/admit", { params: { path: { userID } }, body: { role: "member" } });
  return data?.user as ManagedUser;
}

/** Turns a waiting person away; they may ask again by signing in again. */
export async function declineJoinRequest(userID: string): Promise<void> {
  await api.DELETE("/users/requests/{userID}", { params: { path: { userID } } });
}
