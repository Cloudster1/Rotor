import { expect, type Page } from "@playwright/test";

/** How long a run may take, sandbox start included, before a test gives up on it. */
export const RUN_TIMEOUT_MS = 120_000;

/** Saves a workflow as whoever the page is signed in as, and answers its id. */
export async function createWorkflow(page: Page, source: string): Promise<string> {
  const response = await page.request.post("/api/v1/workflows", { data: { specYAML: source, comment: "e2e" } });
  expect(response.status(), await response.text()).toBe(201);
  const body = (await response.json()) as { workflow: { id: string } };
  return body.workflow.id;
}

/** Starts a run of a workflow straight through the API, as the page's person, and answers its id. */
export async function startRun(page: Page, workflowID: string): Promise<string> {
  const response = await page.request.post(`/api/v1/workflows/${workflowID}/runs`, { data: { inputs: {} } });
  expect(response.status(), await response.text()).toBe(201);
  const body = (await response.json()) as { id: string };
  return body.id;
}
