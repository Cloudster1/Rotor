import { expect, type APIRequestContext, type Page } from "@playwright/test";
import { keycloakURL, WEB_URL } from "./stack";

/** The realm's people, from deploy/keycloak/realm.json; each password is "<name> password". */
export const PEOPLE = {
  alice: { username: "alice", name: "Alice Admin", role: "Administrator" },
  bob: { username: "bob", name: "Bob Builder", role: "Member" },
  carol: { username: "carol", name: "Carol Checker", role: "Member" },
  // Signed in by the Users spec alone, which switches him off and on; no other spec may rely on him.
  dave: { username: "dave", name: "Dave Visitor", role: "Member" },
} as const;
export type Person = (typeof PEOPLE)[keyof typeof PEOPLE];

/** Where the setup project keeps a signed-in browser for the specs that only need one. */
export const ALICE_STATE = ".auth/alice.json";

/** Signs in through Keycloak's own page, as a person would, from the sign-in page of Rotor. */
export async function signIn(page: Page, person: { username: string }, next = "/"): Promise<void> {
  await page.goto(`/login?next=${encodeURIComponent(next)}`);
  await page.locator('[data-action="sso"]').click();
  await page.waitForURL(`${keycloakURL()}/**`);
  await page.locator("#username").fill(person.username);
  await page.locator("#password").fill(`${person.username} password`);
  await page.locator("#kc-login").click();
  await page.waitForURL(`${WEB_URL}/**`);
  await expect(page.locator("main#main")).toBeVisible();
}

/** The stack's Keycloak administrator's header, for its admin API. */
async function keycloakAdmin(request: APIRequestContext): Promise<{ Authorization: string }> {
  const token = await request.post(`${keycloakURL()}/realms/master/protocol/openid-connect/token`, {
    form: { grant_type: "password", client_id: "admin-cli", username: "admin", password: "admin" },
  });
  expect(token.ok(), "Keycloak's admin token").toBe(true);
  return { Authorization: `Bearer ${(await token.json()).access_token}` };
}

/** A person of the realm Rotor has never let in, made for one spec: they wait at their first sign-in (#204). */
export interface Stranger {
  username: string;
  name: string;
  email: string;
}

/** Makes a person in the realm with a name of their own, whose password is "<username> password". */
export async function addStranger(request: APIRequestContext, prefix: string): Promise<Stranger> {
  const username = `${prefix}-${Math.random().toString(36).slice(2, 10)}`;
  const made = await request.post(`${keycloakURL()}/admin/realms/dynamo/users`, {
    headers: await keycloakAdmin(request),
    data: {
      username,
      email: `${username}@suite.test`,
      firstName: "Erin",
      lastName: username,
      enabled: true,
      emailVerified: true,
      credentials: [{ type: "password", value: `${username} password`, temporary: false }],
    },
  });
  expect(made.status(), `making ${username} in the realm`).toBe(201);
  return { username, name: `Erin ${username}`, email: `${username}@suite.test` };
}

/** Removes a person a spec made from the realm; Rotor keeps their account, as it keeps everybody's. */
export async function removeStranger(request: APIRequestContext, stranger: Stranger): Promise<void> {
  const headers = await keycloakAdmin(request);
  const base = `${keycloakURL()}/admin/realms/dynamo/users`;
  const [user] = (await (await request.get(`${base}?username=${stranger.username}&exact=true`, { headers })).json()) as Array<{ id: string }>;
  if (user) await request.delete(`${base}/${user.id}`, { headers });
}

/** Ends every Keycloak session of a person through the admin API, one by one, as an administrator would. */
export async function endProviderSessions(request: APIRequestContext, username: string): Promise<number> {
  const base = keycloakURL();
  const headers = await keycloakAdmin(request);
  const users = await request.get(`${base}/admin/realms/dynamo/users?username=${username}&exact=true`, { headers });
  const [user] = (await users.json()) as Array<{ id: string }>;
  expect(user, `the realm has ${username}`).toBeDefined();
  const sessions = (await (await request.get(`${base}/admin/realms/dynamo/users/${user!.id}/sessions`, { headers })).json()) as Array<{ id: string }>;
  for (const session of sessions) {
    const ended = await request.delete(`${base}/admin/realms/dynamo/sessions/${session.id}`, { headers });
    expect(ended.ok(), `ending session ${session.id}`).toBe(true);
  }
  return sessions.length;
}
