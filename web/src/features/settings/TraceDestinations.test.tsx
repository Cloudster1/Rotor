import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { TraceDestination } from "@/api/traceDestinations";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt, TRACE_SETTING_DEFAULT } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const t = s.traceDestinations;
const BASE = "/api/v1/settings/trace-destinations";
const SECRET = "Basic c2VjcmV0LXZhbHVl";

const JAEGER: TraceDestination = {
  id: "0190a5c4-0000-7000-8000-0000000000d1",
  name: "Jaeger",
  endpoint: "http://jaeger:4318/v1/traces",
  headerNames: ["Authorization"],
  headersSetAt: "2026-10-19T08:00:00Z",
  enabled: true,
  content: true,
  contentAcknowledgedBy: "Ada Admin",
  contentAcknowledgedAt: "2026-10-19T08:00:00Z",
  traceLink: "http://localhost:16686/trace/{traceId}",
  carriedOver: false,
  changedBy: "Ada Admin",
  updatedAt: "2026-10-19T08:00:00Z",
};

/** A stand-in server holding destinations, recording the bodies it is sent. */
function server(initial: TraceDestination[]) {
  let held = [...initial];
  const sent: { method: string; body: Record<string, unknown> }[] = [];
  const answers = {
    [BASE]: () => Response.json({ destinations: held }),
    [`POST ${BASE}`]: async (request: Request) => {
      const body = (await request.json()) as Record<string, unknown>;
      sent.push({ method: "POST", body });
      if (body.content && !body.acknowledgeContentLeaves) {
        return Response.json(
          { error: { code: "content_leaves_unacknowledged", message: "Confirm that text may leave.", fields: { acknowledgeContentLeaves: "Confirm it." } } },
          { status: 422 },
        );
      }
      const names = Object.keys((body.headers as object | undefined) ?? {});
      const created = {
        ...JAEGER,
        id: `id-${held.length}`,
        name: String(body.name),
        endpoint: String(body.endpoint),
        headerNames: names,
        content: Boolean(body.content),
        traceLink: body.traceLink as string | null,
      };
      held = [...held, created];
      return Response.json({ destination: created }, { status: 201 });
    },
    [`PUT ${BASE}/${JAEGER.id}`]: async (request: Request) => {
      const body = (await request.json()) as Record<string, unknown>;
      sent.push({ method: "PUT", body });
      held = held.map((each) => (each.id === JAEGER.id ? { ...each, name: String(body.name) } : each));
      return Response.json({ destination: held[0] });
    },
    [`DELETE ${BASE}/${JAEGER.id}`]: () => {
      sent.push({ method: "DELETE", body: {} });
      held = held.filter((each) => each.id !== JAEGER.id);
      return new Response(null, { status: 204 });
    },
    [`POST ${BASE}/${JAEGER.id}/test`]: () => Response.json({ delivered: true, status: 200, message: "The tool took the span." }),
  };
  return { answers, sent };
}

async function open(initial: TraceDestination[], extra: Record<string, unknown> = {}) {
  const stand = server(initial);
  await renderAt("/admin/traces", { answers: { ...stand.answers, ...extra } });
  const section = await screen.findByRole("region", { name: t.title });
  return { ...stand, section };
}

describe("the trace destinations", () => {
  it("lists each destination with its header names and never a value", async () => {
    const { section } = await open([JAEGER, { ...JAEGER, id: "2", name: "LangSmith", carriedOver: true, content: false, enabled: false, headerNames: [] }]);
    const row = await within(section).findByText("Jaeger");
    expect(row).toBeInTheDocument();
    expect(within(section).getByText(t.headersHeld(["Authorization"]))).toBeInTheDocument();
    expect(within(section).getByText(t.carriedOver)).toBeInTheDocument();
    expect(within(section).getByText(t.off)).toBeInTheDocument();
    expect(within(section).getByText(t.noText)).toBeInTheDocument();
    expect(section.textContent).not.toContain(SECRET);
    expect(await axeViolations()).toEqual([]);
  });

  it("adds a destination from a preset, with its headers as password fields", async () => {
    const { section, sent } = await open([]);
    await userEvent.click(await within(section).findByRole("button", { name: t.add }));
    await userEvent.click(within(section).getByRole("button", { name: s.traceDestinations.presets.langfuse }));
    expect(within(section).getByLabelText(t.form.endpoint)).toHaveValue("https://cloud.langfuse.com/api/public/otel/v1/traces");
    expect(within(section).getByLabelText(`${t.form.headerName} 1`)).toHaveValue("Authorization");
    const value = within(section).getByLabelText(`${t.form.headerValue} 1`);
    expect(value).toHaveAttribute("type", "password");
    expect(value).toHaveAttribute("autocomplete", "off");
    await userEvent.type(value, SECRET);
    await userEvent.click(within(section).getByRole("button", { name: t.form.save }));
    await within(section).findByText(t.headersHeld(["Authorization"]));
    expect(sent[0]?.body).toMatchObject({
      name: "Langfuse",
      headers: { Authorization: SECRET },
      enabled: true,
      content: false,
      acknowledgeContentLeaves: false,
    });
    expect(section.textContent).not.toContain(SECRET);
  });

  it("will not send text before the administrator confirms it leaves, and shows the server's refusal", async () => {
    const { section, sent } = await open([]);
    await userEvent.click(await within(section).findByRole("button", { name: t.add }));
    await userEvent.type(within(section).getByLabelText(t.form.name), "Tool");
    await userEvent.type(within(section).getByLabelText(t.form.endpoint), "http://tool:4318/v1/traces");
    await userEvent.click(within(section).getByLabelText(t.form.content));
    const acknowledge = within(section).getByLabelText(t.form.acknowledge);
    expect(acknowledge).toBeRequired();
    await userEvent.click(within(section).getByRole("button", { name: t.form.save }));
    expect(await within(section).findByText(t.form.acknowledgeMissing)).toBeInTheDocument();
    expect(sent).toHaveLength(0);
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(acknowledge);
    await userEvent.click(within(section).getByRole("button", { name: t.form.save }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]?.body).toMatchObject({ content: true, acknowledgeContentLeaves: true });
  });

  it("keeps the headers held when a destination is changed without touching them", async () => {
    const { section, sent } = await open([JAEGER]);
    await userEvent.click(await within(section).findByRole("button", { name: `${t.edit} Jaeger` }));
    const name = within(section).getByLabelText(t.form.name);
    await userEvent.clear(name);
    await userEvent.type(name, "Jaeger two");
    await userEvent.click(within(section).getByRole("button", { name: t.form.save }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]?.method).toBe("PUT");
    expect(sent[0]?.body).not.toHaveProperty("headers");
    expect(sent[0]?.body).toMatchObject({ name: "Jaeger two", content: true, acknowledgeContentLeaves: true });
  });

  it("removes every header when asked to", async () => {
    const { section, sent } = await open([JAEGER]);
    await userEvent.click(await within(section).findByRole("button", { name: `${t.edit} Jaeger` }));
    await userEvent.click(within(section).getByLabelText(t.form.removeHeaders));
    await userEvent.click(within(section).getByRole("button", { name: t.form.save }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]?.body.headers).toEqual({});
  });

  it("tests a destination and says what came back", async () => {
    const { section } = await open([JAEGER]);
    await userEvent.click(await within(section).findByRole("button", { name: `${t.test} Jaeger` }));
    expect(await within(section).findByText(t.testDelivered("The tool took the span."))).toBeInTheDocument();
  });

  it("removes a destination only after asking", async () => {
    const { section, sent } = await open([JAEGER]);
    await userEvent.click(await within(section).findByRole("button", { name: `${t.remove} Jaeger` }));
    expect(sent).toHaveLength(0);
    await userEvent.click(within(section).getByRole("button", { name: t.confirm }));
    expect(await within(section).findByText(t.none)).toBeInTheDocument();
    expect(sent.map((each) => each.method)).toEqual(["DELETE"]);
  });

  it("offers no more once the most there can be are set", async () => {
    const many = Array.from({ length: 8 }, (_, i) => ({ ...JAEGER, id: `d${i}`, name: `Tool ${i}` }));
    const { section } = await open(many);
    expect(await within(section).findByRole("button", { name: t.add })).toBeDisabled();
    expect(within(section).getByText(t.full(8))).toBeInTheDocument();
  });

  it("says so when OpenBao is missing", async () => {
    const unavailable = { error: { code: "trace_destinations_unavailable", message: "Trace destinations need OpenBao, which is not set up." } };
    await renderAt("/admin/traces", { answers: { [BASE]: () => Response.json(unavailable, { status: 503 }) } });
    expect(await screen.findByText(unavailable.error.message)).toBeInTheDocument();
  });
});

describe("keeping traces", () => {
  it("saves how long traces and their text are kept", async () => {
    const sent: unknown[] = [];
    let current: Record<string, unknown> = { ...TRACE_SETTING_DEFAULT };
    await renderAt("/admin/traces", {
      answers: {
        "/api/v1/settings/traces": () => Response.json(current),
        "PUT /api/v1/settings/traces": async (request: Request) => {
          const body = (await request.json()) as { retentionDays: number; keepContent: boolean };
          sent.push(body);
          current = { ...current, ...body, changedBy: "Ada Admin", updatedAt: "2026-10-19T09:00:00Z" };
          return Response.json(current);
        },
      },
    });
    const section = await screen.findByRole("region", { name: t.keeping.title });
    const days = await within(section).findByLabelText(t.keeping.days);
    await userEvent.clear(days);
    await userEvent.type(days, "30");
    await userEvent.click(within(section).getByLabelText(t.keeping.keepText));
    await userEvent.click(within(section).getByRole("button", { name: t.keeping.save }));
    await waitFor(() => expect(within(section).getByText(new RegExp(t.keeping.changedBy("Ada Admin")))).toBeInTheDocument());
    expect(sent).toEqual([{ retentionDays: 30, keepContent: true }]);
  });

  it("refuses members", async () => {
    await renderAt("/admin/traces", { me: { ...ME, role: "member" } });
    expect(await screen.findByRole("alert")).toHaveTextContent(s.admin.notAllowed);
    expect(screen.queryByRole("region", { name: t.title })).toBeNull();
  });
});
