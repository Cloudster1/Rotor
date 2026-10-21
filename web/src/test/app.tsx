import { render, screen } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { vi } from "vitest";
import type { Me } from "@/api/auth";
import type { Meta } from "@/api/meta";
import { createQueryClient } from "@/lib/session";
import { buildRouter, sendToLogin } from "@/routes";

/** What the stand-in server says about itself unless a test says otherwise. */
export const META: Meta = {
  product: "rotor",
  build: { version: "0.1.0+test", commit: "abc1234" },
  apiVersion: "1",
  capabilities: [],
};

/** Who the stand-in server says is signed in unless a test signs nobody in. */
export const ME: Me = {
  user: { id: "0190a5c4-0000-7000-8000-000000000001", email: "ada@suite.test", name: "Ada Admin" },
  role: "admin",
  groups: ["rotor-administrators", "engineering"],
  assistant: null,
};

/** Assistants as a fresh installation has them: free to connect (#47). */
export const ASSISTANTS_ON = { connectionsOn: true, updatedAt: "2026-10-21T08:00:00Z" };

/** Retention as a fresh installation has it: 90 days, set by nobody (#41). */
export const RETENTION_DEFAULT = { runDataDays: 90, maxDays: 3650, changedBy: null, updatedAt: "2026-10-19T08:00:00Z" };

/** LangSmith as a fresh installation has it: off, with no key. */
export const LANGSMITH_OFF = {
  enabled: false,
  project: "rotor",
  keySet: false,
  keySetAt: null,
  changedBy: null,
  updatedAt: "2026-10-19T08:00:00Z",
  endpoint: "https://api.smith.langchain.com/otel/v1/traces",
};

const NOBODY = { error: { code: "unauthorized", message: "Sign in to continue." } };

/** An answer: a body sent as JSON with 200, or a function of the request for anything else. */
export type Answer = unknown | ((request: Request) => Response | Promise<Response>);

/**
 * Answers the client's requests from a table keyed by "METHOD /path" or by path alone; anything else is a 404
 * in the error envelope.
 */
export function stubApi(answers: Record<string, Answer> = { "/api/v1/meta": META, "/api/v1/auth/me": ME }) {
  // Like the real server, nobody is signed in any more once the session is ended.
  let signedOut = false;
  const fetch = vi.fn(async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (path === "/api/v1/auth/logout") {
      signedOut = true;
      return new Response(null, { status: 204 });
    }
    if (path === "/api/v1/auth/me" && (answers[path] === null || signedOut)) return Response.json(NOBODY, { status: 401 });
    const key = `${request.method} ${path}`;
    const answer = key in answers ? answers[key] : request.method === "GET" && path in answers ? answers[path] : undefined;
    if (typeof answer === "function") return (answer as (request: Request) => Response | Promise<Response>)(request);
    if (answer !== undefined) return Response.json(answer);
    // A workflow's page lists its triggers; it has none unless a test says so.
    if (request.method === "GET" && /^\/api\/v1\/workflows\/[^/]+\/triggers$/.test(path)) return Response.json({ triggers: [] });
    return Response.json({ error: { code: "not_found", message: "Not found." } }, { status: 404 });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

/** Starts the real router at a path, against a stand-in server, and waits for the page to draw; me null signs nobody in. */
export async function renderAt(path: string, { me = ME as Me | null, answers = {} as Record<string, Answer> } = {}) {
  const fetch = stubApi({
    "/api/v1/meta": META,
    "/api/v1/auth/me": me,
    "/api/v1/tools": { tools: [] },
    "/api/v1/connections": { connections: [] },
    "/api/v1/providers": { providers: [] },
    "/api/v1/workflows": { workflows: [] },
    "/api/v1/runs": { runs: [] },
    // The sidebar asks for the badge's number on every page.
    "/api/v1/approvals/count": { waitingOnMe: 0 },
    "/api/v1/approvals": { approvals: [] },
    "/api/v1/triggers": { triggers: [] },
    // An administrator's settings show whether runs go to LangSmith (#37); off unless a test says otherwise.
    "/api/v1/settings/langsmith": LANGSMITH_OFF,
    "/api/v1/settings/retention": RETENTION_DEFAULT,
    "/api/v1/oauth/grants": { grants: [] },
    "/api/v1/oauth/grants/everyone": { grants: [] },
    "/api/v1/settings/assistants": ASSISTANTS_ON,
    "/api/v1/schemas/workflow.v1.json": { $id: "https://rotor.dev/schemas/workflow.v1.json", type: "object" },
    "POST /api/v1/workflows/validate": { valid: true, problems: [], warnings: [] },
    ...answers,
  });
  // The router is made right below; a 401 can only arrive once it exists.
  const queryClient = createQueryClient((client) => sendToLogin(router, client));
  const router = buildRouter(queryClient, createMemoryHistory({ initialEntries: [path] }));
  const view = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await screen.findByRole("main");
  return { ...view, router, fetch };
}
