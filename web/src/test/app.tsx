import { render, screen } from "@testing-library/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import { vi } from "vitest";
import type { Meta } from "@/api/meta";
import { createQueryClient } from "@/lib/session";
import { buildRouter } from "@/routes";

/** What the stand-in server says about itself unless a test says otherwise. */
export const META: Meta = {
  product: "rotor",
  build: { version: "0.1.0+test", commit: "abc1234" },
  apiVersion: "1",
  capabilities: [],
};

/** Answers the client's requests from a table of path to body; anything else is a 404 in the error envelope. */
export function stubApi(answers: Record<string, unknown> = { "/api/v1/meta": META }) {
  const fetch = vi.fn(async (request: Request) => {
    const path = new URL(request.url).pathname;
    if (path in answers) return Response.json(answers[path]);
    return Response.json({ error: { code: "not_found", message: "Not found." } }, { status: 404 });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

/** Starts the real router at a path, against a stand-in server, and waits for the page to draw. */
export async function renderAt(path: string) {
  stubApi();
  const queryClient = createQueryClient(() => {});
  const router = buildRouter(queryClient, createMemoryHistory({ initialEntries: [path] }));
  const view = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await screen.findByRole("main");
  return { ...view, router };
}
