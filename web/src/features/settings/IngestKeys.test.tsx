import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { IngestKey } from "@/api/ingestKeys";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const t = s.traceDestinations.ingestKeys;
const BASE = "/api/v1/settings/ingest-keys";
const SECRET = "rotor_ik_c2VjcmV0LXZhbHVlLXRoYXQtaXMtc2hvd24tb25jZQabcd";
const ROTATED = "rotor_ik_YW5vdGhlci1zZWNyZXQtdGhhdC1pcy1zaG93bi1vbmNlwxyz";

const BILLING: IngestKey = {
  id: "0190a5c4-0000-7000-8000-0000000000e1",
  name: "Billing service",
  account: { id: "0190a5c4-0000-7000-8000-0000000000a1", name: "billing-bot", email: "billing-bot@example.test" },
  hint: "abcd",
  expiresAt: null,
  expired: false,
  lastUsedAt: null,
  createdBy: "Ada Admin",
  createdAt: "2026-11-01T08:00:00Z",
  rotatedAt: null,
};

/** A stand-in server holding keys, recording the requests it is sent. */
function server(initial: IngestKey[]) {
  let held = [...initial];
  const sent: { method: string; path: string; body?: Record<string, unknown> }[] = [];
  const answers = {
    [BASE]: () => Response.json({ keys: held }),
    [`POST ${BASE}`]: async (request: Request) => {
      const body = (await request.json()) as Record<string, unknown>;
      sent.push({ method: "POST", path: BASE, body });
      if (!body.name) {
        return Response.json({ error: { code: "validation_failed", message: "Check the fields.", fields: { name: "Name the key." } } }, { status: 422 });
      }
      const made = { ...BILLING, id: `k${held.length}`, name: String(body.name), hint: SECRET.slice(-4) };
      held = [...held, made];
      return Response.json({ key: made, secret: SECRET }, { status: 201 });
    },
    [`POST ${BASE}/${BILLING.id}/rotate`]: () => {
      sent.push({ method: "POST", path: "rotate" });
      held = held.map((each) => (each.id === BILLING.id ? { ...each, hint: ROTATED.slice(-4), rotatedAt: "2026-11-06T09:00:00Z" } : each));
      return Response.json({ key: held.find((each) => each.id === BILLING.id), secret: ROTATED });
    },
    [`DELETE ${BASE}/${BILLING.id}`]: () => {
      sent.push({ method: "DELETE", path: BILLING.id });
      held = held.filter((each) => each.id !== BILLING.id);
      return new Response(null, { status: 204 });
    },
  };
  return { answers, sent };
}

async function open(initial: IngestKey[]) {
  const stand = server(initial);
  await renderAt("/admin/traces", { answers: stand.answers });
  const section = await screen.findByRole("region", { name: t.title });
  return { ...stand, section };
}

describe("the ingest keys", () => {
  it("lists each key with whom it sends as and its last characters, never the key", async () => {
    const used = { ...BILLING, id: "k2", name: "Search", lastUsedAt: "2026-11-05T10:00:00Z", expiresAt: "2026-11-02T00:00:00Z", expired: true };
    const { section } = await open([BILLING, used]);
    expect(await within(section).findByText("Billing service")).toBeInTheDocument();
    expect(within(section).getAllByText(t.sendsAs("billing-bot", "billing-bot@example.test"))).toHaveLength(2);
    expect(within(section).getAllByText(t.endsIn("abcd"))).toHaveLength(2);
    expect(within(section).getByText(t.neverUsed)).toBeInTheDocument();
    expect(within(section).getByText(t.expired)).toBeInTheDocument();
    // An expired key is not offered a new secret, which would not send.
    expect(within(section).queryByRole("button", { name: `${t.rotate} Search` })).toBeNull();
    expect(section.textContent).not.toContain("rotor_ik_");
    expect(await axeViolations()).toEqual([]);
  });

  it("makes a key for an account and shows it once, with the header to set", async () => {
    const { section, sent } = await open([]);
    expect(await within(section).findByText(t.none)).toBeInTheDocument();
    await userEvent.click(within(section).getByRole("button", { name: t.make }));
    await userEvent.type(within(section).getByLabelText(t.name), "Search");
    await userEvent.type(within(section).getByLabelText(t.account), "search-bot@example.test");
    expect(await axeViolations()).toEqual([]);
    await userEvent.click(within(section).getByRole("button", { name: t.save }));

    const secret = await within(section).findByLabelText(t.secretLabel("Search"));
    expect(secret).toHaveValue(SECRET);
    expect(secret).toHaveAttribute("readonly");
    expect(within(section).getByText(`OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20${SECRET}`)).toBeInTheDocument();
    expect(sent[0]?.body).toEqual({ name: "Search", accountEmail: "search-bot@example.test" });
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(section).getByRole("button", { name: t.saved }));
    await waitFor(() => expect(section.textContent).not.toContain(SECRET));
    expect(within(section).getByText(t.endsIn(SECRET.slice(-4)))).toBeInTheDocument();
  });

  it("sends an expiry as an instant and shows the server's refusal beside the field", async () => {
    const { section, sent } = await open([]);
    await userEvent.click(await within(section).findByRole("button", { name: t.make }));
    await userEvent.type(within(section).getByLabelText(t.expires), "2030-01-02T03:04");
    await userEvent.click(within(section).getByRole("button", { name: t.save }));
    expect(await within(section).findByText("Name the key.")).toBeInTheDocument();
    expect(sent[0]?.body).toEqual({ name: "", expiresAt: new Date("2030-01-02T03:04").toISOString() });
    expect(within(section).queryByLabelText(t.secretLabel(""))).toBeNull();
  });

  it("rotates a key only after asking, and shows the new key once", async () => {
    const { section, sent } = await open([BILLING]);
    await userEvent.click(await within(section).findByRole("button", { name: `${t.rotate} Billing service` }));
    expect(sent).toHaveLength(0);
    await userEvent.click(within(section).getByRole("button", { name: t.rotateIt }));
    expect(await within(section).findByLabelText(t.secretLabel("Billing service"))).toHaveValue(ROTATED);
    expect(sent.map((each) => each.path)).toEqual(["rotate"]);
  });

  it("revokes a key only after asking", async () => {
    const { section, sent } = await open([BILLING]);
    await userEvent.click(await within(section).findByRole("button", { name: `${t.revoke} Billing service` }));
    await userEvent.click(within(section).getByRole("button", { name: t.keep }));
    expect(sent).toHaveLength(0);
    await userEvent.click(within(section).getByRole("button", { name: `${t.revoke} Billing service` }));
    await userEvent.click(within(section).getByRole("button", { name: t.revokeIt }));
    expect(await within(section).findByText(t.none)).toBeInTheDocument();
    expect(sent.map((each) => each.method)).toEqual(["DELETE"]);
  });

  it("offers no more once the most there can be are made", async () => {
    const many = Array.from({ length: 50 }, (_, i) => ({ ...BILLING, id: `k${i}`, name: `Service ${i}` }));
    const { section } = await open(many);
    expect(await within(section).findByRole("button", { name: t.make })).toBeDisabled();
    expect(within(section).getByText(t.full(50))).toBeInTheDocument();
  });

  it("is not shown to members", async () => {
    await renderAt("/admin/traces", { me: { ...ME, role: "member" } });
    expect(await screen.findByRole("alert")).toHaveTextContent(s.admin.notAllowed);
    expect(screen.queryByRole("region", { name: t.title })).toBeNull();
  });
});
