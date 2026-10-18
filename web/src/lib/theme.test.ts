import { afterEach, describe, expect, it, vi } from "vitest";
import { THEME_STORAGE_KEY } from "@/config";
import { applyTheme, nextTheme, readTheme, saveTheme, THEME_CHOICES } from "./theme";

afterEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
  vi.restoreAllMocks();
});

describe("the colour scheme", () => {
  it("follows the system until a choice is stored", () => {
    expect(readTheme()).toBe("system");
    localStorage.setItem(THEME_STORAGE_KEY, "nonsense");
    expect(readTheme()).toBe("system");
  });

  it("remembers a choice and marks the document with it", () => {
    saveTheme("dark");
    expect(readTheme()).toBe("dark");
    expect(document.documentElement).toHaveAttribute("data-theme", "dark");
    saveTheme("system");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
    expect(document.documentElement).not.toHaveAttribute("data-theme");
  });

  it("steps through every choice and back to the first", () => {
    let choice = THEME_CHOICES[0]!;
    const seen = [choice];
    for (let i = 1; i < THEME_CHOICES.length; i++) {
      choice = nextTheme(choice);
      seen.push(choice);
    }
    expect(seen).toEqual([...THEME_CHOICES]);
    expect(nextTheme(choice)).toBe(THEME_CHOICES[0]);
  });

  it("still applies a choice when the browser refuses storage", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("refused", "SecurityError");
    });
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("refused", "SecurityError");
    });
    expect(() => saveTheme("light")).not.toThrow();
    expect(document.documentElement).toHaveAttribute("data-theme", "light");
    expect(readTheme()).toBe("system");
    applyTheme("system");
    expect(document.documentElement).not.toHaveAttribute("data-theme");
  });
});
