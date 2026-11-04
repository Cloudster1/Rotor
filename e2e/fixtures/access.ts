import { type Browser, expect, type Page } from "@playwright/test";
import { type Person, signIn } from "./auth";

/** Signs a person in once in a browser of their own, so Rotor knows them and a picker can find them. */
export async function makeKnown(browser: Browser, person: Person): Promise<void> {
  const context = await browser.newContext({ storageState: { cookies: [], origins: [] } });
  try {
    await signIn(await context.newPage(), person);
  } finally {
    await context.close();
  }
}

/** A workflow with no tools, which saves whatever tools the shared stack has. */
export function plainWorkflow(name: string): string {
  return [
    "apiVersion: rotor.dev/v1",
    "kind: Workflow",
    `metadata: {name: ${name}}`,
    "limits: {maxSteps: 3, timeout: 1m}",
    "entry: ask",
    "nodes:",
    "  - {id: ask, type: approval, message: 'Go on?'}",
    "",
  ].join("\n");
}

/** Takes a role a spec made away again through the API, so a rerun starts clean; a role already gone is fine. */
export async function deleteRoleNamed(page: Page, name: string): Promise<void> {
  const roles = (await (await page.request.get("/api/v1/roles")).json()) as { roles: { key: string; name: string }[] };
  for (const role of roles.roles.filter((each) => each.name === name)) {
    const gone = await page.request.delete(`/api/v1/roles/${role.key}`);
    expect([204, 404], `deleting role ${name}`).toContain(gone.status());
  }
}

/** Deletes a group a spec made, through the API. */
export async function deleteGroupNamed(page: Page, name: string): Promise<void> {
  const groups = (await (await page.request.get("/api/v1/groups")).json()) as { groups: { id: string; name: string }[] };
  for (const group of groups.groups.filter((each) => each.name === name)) {
    const gone = await page.request.delete(`/api/v1/groups/${group.id}`);
    expect([204, 404], `deleting group ${name}`).toContain(gone.status());
  }
}

/** Archives a workflow a spec made, which takes it and its grants out of everybody's lists. */
export async function archiveWorkflow(page: Page, id: string): Promise<void> {
  const gone = await page.request.delete(`/api/v1/workflows/${id}`);
  expect([204, 404], `archiving workflow ${id}`).toContain(gone.status());
}

/** Switches a person on again through the API, whatever a failed spec left them as. */
export async function switchOn(page: Page, email: string): Promise<void> {
  const users = (await (await page.request.get(`/api/v1/users?q=${encodeURIComponent(email)}`)).json()) as { users: { id: string; email: string }[] };
  for (const user of users.users.filter((each) => each.email === email)) {
    const on = await page.request.patch(`/api/v1/users/${user.id}`, { data: { isActive: true } });
    expect(on.status(), `switching ${email} on`).toBe(200);
  }
}
