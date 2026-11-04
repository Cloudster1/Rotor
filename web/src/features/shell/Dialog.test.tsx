import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { ConfirmDialog } from "./Dialog";

afterEach(cleanup);

function open(props: Partial<Parameters<typeof ConfirmDialog>[0]> = {}) {
  const onConfirm = vi.fn();
  const onClose = vi.fn();
  render(<ConfirmDialog title="Archive triage?" body="It leaves the list." action="Archive workflow" onConfirm={onConfirm} onClose={onClose} {...props} />);
  return { onConfirm, onClose, dialog: screen.getByRole("dialog", { name: "Archive triage?" }) };
}

describe("a ConfirmDialog", () => {
  it("opens as a modal that says what goes with the act, and passes axe", async () => {
    const { dialog } = open();
    expect(dialog).toHaveAttribute("open");
    expect(dialog).toHaveAccessibleDescription("It leaves the list.");
    expect(await axeViolations()).toEqual([]);
  });

  it("does the act only on its button, and Cancel closes it without", async () => {
    const { dialog, onConfirm, onClose } = open();
    await userEvent.click(within(dialog).getByRole("button", { name: s.shell.cancel }));
    expect(onClose).toHaveBeenCalled();
    expect(onConfirm).not.toHaveBeenCalled();
    cleanup();
    const again = open();
    await userEvent.click(within(again.dialog).getByRole("button", { name: "Archive workflow" }));
    expect(again.onConfirm).toHaveBeenCalledTimes(1);
  });

  it("says what the server refused, and offers no second act while the first is on its way", () => {
    const { dialog } = open({ pending: true, error: new Error("Your grants do not let you do this.") });
    expect(within(dialog).getByRole("alert")).toHaveTextContent("Your grants do not let you do this.");
    expect(within(dialog).getByRole("button", { name: "Archive workflow" })).toBeDisabled();
  });
});
