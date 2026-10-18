import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("signing in", () => {
  it("sends a visitor without a session to sign in, and back where they were headed", async () => {
    const { router } = await renderAt("/runs?status=failed", { me: null });
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(await screen.findByRole("heading", { level: 1, name: s.signIn.title("Rotor") })).toBeInTheDocument();
    const sso = screen.getByRole("link", { name: s.signIn.action });
    expect(sso).toHaveAttribute("href", `/api/v1/auth/oidc/start?next=${encodeURIComponent("/runs?status=failed")}`);
  });

  it("says why the last attempt failed", async () => {
    await renderAt("/login?error=switched_off", { me: null });
    expect(await screen.findByRole("alert")).toHaveTextContent(s.signIn.errors.switched_off);
  });

  it("names an unknown failure in general words, and never leads to another site", async () => {
    await renderAt("/login?error=gremlins&next=https://evil.test", { me: null });
    expect(await screen.findByRole("alert")).toHaveTextContent(s.signIn.errors.failed);
    expect(screen.getByRole("link", { name: s.signIn.action })).toHaveAttribute("href", "/api/v1/auth/oidc/start");
  });

  it("sends somebody already signed in straight on", async () => {
    const { router } = await renderAt("/login?next=/approvals");
    await waitFor(() => expect(router.state.location.pathname).toBe("/approvals"));
  });

  it("shows who is signed in, and signs them out", async () => {
    const { router, fetch } = await renderAt("/workflows");
    expect(await screen.findByText(ME.user.name)).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: s.signIn.signOut }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    const calls = fetch.mock.calls.map(([request]) => `${request.method} ${new URL(request.url).pathname}`);
    expect(calls).toContain("POST /api/v1/auth/logout");
  });

  it("shows the person's role and groups in the settings", async () => {
    await renderAt("/settings");
    expect(await screen.findByText(s.account.roles.admin)).toBeInTheDocument();
    expect(screen.getByText(ME.groups.join(", "))).toBeInTheDocument();
  });

  it("passes axe on the sign-in page", async () => {
    await renderAt("/login?error=expired", { me: null });
    await screen.findByRole("heading", { level: 1 });
    expect(await axeViolations()).toEqual([]);
  });
});
