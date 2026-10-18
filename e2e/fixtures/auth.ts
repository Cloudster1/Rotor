import { expect, type APIRequestContext, type Page } from "@playwright/test";
import { keycloakURL, WEB_URL } from "./stack";

/** The realm's people, from deploy/keycloak/realm.json; each password is "<name> password". */
export const PEOPLE = {
  alice: { username: "alice", name: "Alice Admin", role: "Administrator" },
  bob: { username: "bob", name: "Bob Builder", role: "Member" },
  carol: { username: "carol", name: "Carol Checker", role: "Member" },
} as const;
export type Person = (typeof PEOPLE)[keyof typeof PEOPLE];

/** Where the setup project keeps a signed-in browser for the specs that only need one. */
export const ALICE_STATE = ".auth/alice.json";

/** Signs in through Keycloak's own page, as a person would, from the sign-in page of Rotor. */
export async function signIn(page: Page, person: Person, next = "/"): Promise<void> {
  await page.goto(`/login?next=${encodeURIComponent(next)}`);
  await page.locator('[data-action="sso"]').click();
  await page.waitForURL(`${keycloakURL()}/**`);
  await page.locator("#username").fill(person.username);
  await page.locator("#password").fill(`${person.username} password`);
  await page.locator("#kc-login").click();
  await page.waitForURL(`${WEB_URL}/**`);
  await expect(page.locator("main#main")).toBeVisible();
}

/** Ends every Keycloak session of a person through the admin API, one by one, as an administrator would. */
export async function endProviderSessions(request: APIRequestContext, username: string): Promise<number> {
  const base = keycloakURL();
  const token = await request.post(`${base}/realms/master/protocol/openid-connect/token`, {
    form: { grant_type: "password", client_id: "admin-cli", username: "admin", password: "admin" },
  });
  expect(token.ok(), "Keycloak's admin token").toBe(true);
  const headers = { Authorization: `Bearer ${(await token.json()).access_token}` };
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
