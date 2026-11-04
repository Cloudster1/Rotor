import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { THEME_STORAGE_KEY } from "@/config";
import { saveTheme } from "@/lib/theme";
import { s } from "@/strings";
import { axeViolations } from "@/test/axe";
import { renderAt } from "@/test/app";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  saveTheme("system");
});

describe("the theme page", () => {
  it("opens with the sentence on what it is for, the choice in force checked", async () => {
    saveTheme("dark");
    await renderAt("/settings/theme");
    expect(await screen.findByRole("heading", { level: 1, name: s.settings.pages.theme })).toBeInTheDocument();
    expect(screen.getByText(s.settings.intros.theme)).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: s.themePage.choices.dark })).toBeChecked();
  });

  it("applies and remembers a choice, and the top bar's button follows it", async () => {
    await renderAt("/settings/theme");
    const button = document.querySelector('[data-action="theme"]') as HTMLElement;
    expect(button).toHaveAttribute("data-theme-choice", "system");
    await userEvent.click(await screen.findByRole("radio", { name: s.themePage.choices.light }));
    expect(document.documentElement).toHaveAttribute("data-theme", "light");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("light");
    expect(button).toHaveAttribute("data-theme-choice", "light");

    await userEvent.click(button);
    expect(screen.getByRole("radio", { name: s.themePage.choices.dark })).toBeChecked();
    await userEvent.click(screen.getByRole("radio", { name: s.themePage.choices.system }));
    expect(document.documentElement).not.toHaveAttribute("data-theme");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
  });

  it("passes axe", async () => {
    await renderAt("/settings/theme");
    await screen.findByRole("radio", { name: s.themePage.choices.system });
    expect(await axeViolations()).toEqual([]);
  });
});
