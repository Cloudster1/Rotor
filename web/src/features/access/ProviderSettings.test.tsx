import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ProviderInput, SingleSignOn } from "@/api/sso";
import { s } from "@/strings";
import { ACCESS_ANSWERS, SSO } from "@/test/access";
import { renderAt } from "@/test/app";
import { axeViolations } from "@/test/axe";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

/** Single sign-on as a server keeps it: a save answers what it holds, never the secret. */
function serveSSO(start: SingleSignOn, refuse?: { status: number; body: unknown }) {
  let held = start;
  const sent: ProviderInput[] = [];
  const answers: Record<string, unknown> = {
    ...ACCESS_ANSWERS,
    "/api/v1/oidc-provider": () => Response.json(held),
    "PUT /api/v1/oidc-provider": async (request: Request) => {
      const input = (await request.json()) as ProviderInput;
      sent.push(input);
      if (refuse) return Response.json(refuse.body, { status: refuse.status });
      held = {
        ...held,
        provider: {
          issuer: input.issuer,
          clientId: input.clientId,
          groupsClaim: input.groupsClaim ?? "groups",
          admitByGroup: input.admitByGroup ?? false,
          enabled: input.enabled ?? true,
          hasSecret: Boolean(held.provider?.hasSecret || input.clientSecret),
          changedBy: "Ada Admin",
          updatedAt: "2026-11-04T10:00:00Z",
        },
      };
      return Response.json(held);
    },
  };
  return { answers, sent };
}

const field = (name: string) => document.querySelector(`[data-sso-field="${name}"]`) as HTMLInputElement;

describe("the Single sign-on tab", () => {
  it("shows the provider with a secret held but never its value, and lets people in by their groups once ticked and saved", async () => {
    const server = serveSSO(SSO);
    await renderAt("/admin/access?tab=sso", { answers: server.answers });
    expect(await screen.findByRole("tab", { name: s.access.tabs.sso, selected: true })).toBeInTheDocument();
    await waitFor(() => expect(field("issuer")).not.toBeNull());
    expect(field("issuer")).toHaveValue(SSO.provider?.issuer);
    expect(field("clientId")).toHaveValue("rotor");
    expect(field("clientSecret")).toHaveValue("");
    expect(field("clientSecret")).toHaveAttribute("type", "password");
    expect(screen.getAllByText(s.sso.secretStored).length).toBeGreaterThan(0);
    expect(screen.getByText(SSO.redirectUrl)).toBeInTheDocument();
    expect(screen.getByText(s.sso.cannotTurnOff)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: s.sso.turnOff })).not.toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    const admit = screen.getByRole("checkbox", { name: s.sso.admit });
    expect(admit).not.toBeChecked();
    await userEvent.click(admit);
    expect(server.sent).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: s.sso.save }));
    await waitFor(() => expect(server.sent).toHaveLength(1));
    // A blank secret is left out, so the one held is kept.
    expect(server.sent[0]).toEqual({ issuer: SSO.provider?.issuer, clientId: "rotor", groupsClaim: "groups", admitByGroup: true, enabled: true });
    expect(await screen.findByText(s.sso.saved)).toBeInTheDocument();
    expect(await screen.findByText(s.sso.changedBy("Ada Admin"))).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: s.sso.admit })).toBeChecked();
  });

  it("sends a new secret once and starts the field empty again", async () => {
    const server = serveSSO(SSO);
    await renderAt("/admin/access?tab=sso", { answers: server.answers });
    await waitFor(() => expect(field("clientSecret")).not.toBeNull());
    await userEvent.type(field("clientSecret"), "s3cret");
    await userEvent.click(screen.getByRole("button", { name: s.sso.save }));
    await waitFor(() => expect(server.sent[0]?.clientSecret).toBe("s3cret"));
    await waitFor(() => expect(field("clientSecret")).toHaveValue(""));
  });

  it("says beside each field what would keep it from working", async () => {
    const refusal = {
      error: {
        code: "validation_failed",
        message: "Single sign-on needs an issuer and a client ID to work. Fill them in, then save again.",
        fields: { issuer: "Give the issuer, the address of the provider's realm, such as https://id.example.com/realms/dynamo." },
      },
    };
    const server = serveSSO(SSO, { status: 422, body: refusal });
    await renderAt("/admin/access?tab=sso", { answers: server.answers });
    await waitFor(() => expect(field("issuer")).not.toBeNull());
    await userEvent.clear(field("issuer"));
    await userEvent.click(screen.getByRole("button", { name: s.sso.save }));
    expect(await screen.findByRole("alert")).toHaveTextContent(refusal.error.message);
    expect(field("issuer")).toHaveAttribute("aria-invalid", "true");
    expect(screen.getByText(refusal.error.fields.issuer)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);
  });

  it("offers setting it up when there is none", async () => {
    const server = serveSSO({ provider: null, redirectUrl: SSO.redirectUrl, canTurnOff: false });
    await renderAt("/admin/access?tab=sso", { answers: server.answers });
    expect(await screen.findByRole("button", { name: s.sso.setUp })).toBeInTheDocument();
    expect(screen.getByText(s.sso.secretMissing)).toBeInTheDocument();
  });

  it("turns it off where another way in remains", async () => {
    const server = serveSSO({ ...SSO, canTurnOff: true });
    await renderAt("/admin/access?tab=sso", { answers: server.answers });
    await userEvent.click(await screen.findByRole("button", { name: s.sso.turnOff }));
    await waitFor(() => expect(server.sent[0]).toMatchObject({ enabled: false }));
    expect(server.sent[0]?.clientSecret).toBeUndefined();
    expect(await screen.findByText(s.sso.offAddress)).toBeInTheDocument();
  });
});
