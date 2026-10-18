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
};

const NOBODY = { error: { code: "unauthorized", message: "Sign in to continue." } };

/** Answers the client's requests from a table of path to body; anything else is a 404 in the error envelope. */
export function stubApi(answers: Record<string, unknown> = { "/api/v1/meta": META, "/api/v1/auth/me": ME }) {
  // Like the real server, nobody is signed in any more once the session is ended.
  let signedOut = false;
  const fetch = vi.fn(async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (path === "/api/v1/auth/logout") {
      signedOut = true;
      return new Response(null, { status: 204 });
    }
    if (path === "/api/v1/auth/me" && (answers[path] === null || signedOut)) return Response.json(NOBODY, { status: 401 });
    if (path in answers) return Response.json(answers[path]);
    return Response.json({ error: { code: "not_found", message: "Not found." } }, { status: 404 });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

/** Starts the real router at a path, against a stand-in server, and waits for the page to draw; me null signs nobody in. */
export async function renderAt(path: string, { me = ME as Me | null } = {}) {
  const fetch = stubApi({ "/api/v1/meta": META, "/api/v1/auth/me": me });
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
