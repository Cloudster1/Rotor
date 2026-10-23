import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ME, WORKSPACES_DEFAULT, renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const DIGEST = "@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c";

const UNPINNED = {
  error: {
    code: "validation_failed",
    message: "Some fields need attention.",
    fields: { "images/0/reference": "Give the image's reference with its digest, so the name always means the same image." },
  },
};

describe("the workspace setting", () => {
  it("lets an administrator allow a pinned toolchain image and name the mirror", async () => {
    const sent: { images: { name: string; reference: string }[]; packageMirror: string }[] = [];
    let current = { ...WORKSPACES_DEFAULT };
    await renderAt("/settings", {
      answers: {
        "/api/v1/settings/workspaces": () => Response.json(current),
        "PUT /api/v1/settings/workspaces": async (request: Request) => {
          const body = (await request.json()) as typeof current;
          sent.push(body);
          if (!body.images.every((i) => i.reference.includes("@sha256:"))) return Response.json(UNPINNED, { status: 422 });
          current = body;
          return Response.json(current);
        },
      },
    });
    const section = await screen.findByRole("region", { name: s.workspaces.title });
    expect(within(section).getByText(s.workspaces.noImages)).toBeInTheDocument();
    expect(await axeViolations()).toEqual([]);

    await userEvent.click(within(section).getByRole("button", { name: s.workspaces.addImage }));
    await userEvent.type(within(section).getByLabelText(s.workspaces.imageName(1)), "golang-1.26");
    await userEvent.type(within(section).getByLabelText(s.workspaces.imageReference(1)), "golang:1.26-alpine");
    await userEvent.type(within(section).getByLabelText(s.workspaces.packageMirror), "https://mirror.example.com/go");
    await userEvent.click(within(section).getByRole("button", { name: s.workspaces.save }));
    expect(await within(section).findByRole("alert")).toBeInTheDocument();
    expect(within(section).getByText(UNPINNED.error.fields["images/0/reference"])).toBeInTheDocument();

    await userEvent.type(within(section).getByLabelText(s.workspaces.imageReference(1)), DIGEST);
    await userEvent.click(within(section).getByRole("button", { name: s.workspaces.save }));
    await waitFor(() => expect(within(section).getByRole("status")).toHaveTextContent(s.workspaces.saved));
    expect(sent.at(-1)).toEqual({
      images: [{ name: "golang-1.26", reference: `golang:1.26-alpine${DIGEST}` }],
      packageMirror: "https://mirror.example.com/go",
      maxStepTime: "1h",
      maxVolume: "1Gi",
    });
    expect(await axeViolations()).toEqual([]);
  });

  it("is not shown to members", async () => {
    await renderAt("/settings", { me: { ...ME, role: "member" } });
    await screen.findByRole("heading", { level: 1 });
    expect(screen.queryByRole("region", { name: s.workspaces.title })).toBeNull();
  });
});
