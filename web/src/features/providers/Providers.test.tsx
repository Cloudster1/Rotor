import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Provider } from "@/api/providers";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const ANTHROPIC: Provider = {
  id: "0190a5c4-0000-7000-8000-00000000a001",
  name: "Anthropic",
  kind: "anthropic",
  baseURL: "",
  apiVersion: "",
  models: ["claude-sonnet-5-5", "claude-haiku-5-5"],
  defaultModel: "claude-sonnet-5-5",
  isDefault: true,
  enabled: true,
  hasKey: true,
};

describe("the model providers", () => {
  it("are for administrators alone", async () => {
    await renderAt("/settings", { me: { ...ME, role: "member", groups: [] } });
    await screen.findByRole("heading", { name: s.tools.title });
    expect(screen.queryByRole("heading", { name: s.providers.title })).not.toBeInTheDocument();
  });

  it("are listed with their models, default and key, and a test says what the provider answered", async () => {
    await renderAt("/settings", {
      answers: {
        "/api/v1/providers": { providers: [ANTHROPIC] },
        [`POST /api/v1/providers/${ANTHROPIC.id}/test`]: { ok: false, message: "The provider refused the key. Check it, then test again.", models: [] },
      },
    });
    await screen.findByText(s.providers.keyHeld);
    const item = document.querySelector('[data-provider="Anthropic"]') as HTMLElement;
    expect(within(item).getByText(s.providers.isDefault)).toBeInTheDocument();
    expect(within(item).getByText(s.providers.keyHeld)).toBeInTheDocument();
    await userEvent.click(within(item).getByRole("button", { name: s.providers.test }));
    expect(await within(item).findByRole("status")).toHaveTextContent("refused the key");
  });

  it("are added with a key that is sent once and never shown", async () => {
    let sent: Record<string, unknown> | undefined;
    let providers: Provider[] = [];
    await renderAt("/settings", {
      answers: {
        "/api/v1/providers": () => Response.json({ providers }),
        "POST /api/v1/providers": async (request: Request) => {
          sent = (await request.json()) as Record<string, unknown>;
          providers = [{ ...ANTHROPIC, id: "p2", name: "Ollama", kind: "openai_compatible", isDefault: false }];
          return Response.json({ provider: providers[0] }, { status: 201 });
        },
      },
    });
    await userEvent.click(await screen.findByRole("button", { name: s.providers.form.addTitle }));
    await userEvent.type(screen.getByLabelText(s.providers.form.name), "Ollama");
    await userEvent.selectOptions(screen.getByLabelText(s.providers.form.kind), "openai_compatible");
    await userEvent.type(screen.getByLabelText(s.providers.form.baseURL), "http://ollama:11434/v1");
    await userEvent.type(screen.getByLabelText(s.providers.form.models), "llama3, qwen3");
    await userEvent.selectOptions(screen.getByLabelText(s.providers.form.defaultModel), "qwen3");
    await userEvent.type(screen.getByLabelText(s.providers.form.apiKey), "sk-local");
    await userEvent.click(screen.getByRole("button", { name: s.providers.form.add }));
    await waitFor(() =>
      expect(sent).toMatchObject({ name: "Ollama", kind: "openai_compatible", models: ["llama3", "qwen3"], defaultModel: "qwen3", apiKey: "sk-local" }),
    );
    await waitFor(() => expect(document.querySelector('[data-provider="Ollama"]')).not.toBeNull());
    expect(screen.queryByDisplayValue("sk-local")).not.toBeInTheDocument();
  });

  it("pass axe with the form open", async () => {
    await renderAt("/settings", { answers: { "/api/v1/providers": { providers: [ANTHROPIC] } } });
    await userEvent.click(await screen.findByRole("button", { name: s.providers.edit }));
    await screen.findByRole("heading", { name: s.providers.form.editTitle("Anthropic") });
    expect(await axeViolations()).toEqual([]);
  });
});
