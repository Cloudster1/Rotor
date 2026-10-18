import { THEME_STORAGE_KEY } from "@/config";

/** What a person may choose: follow the system, or one palette whatever the system says. */
export type ThemeChoice = "system" | "light" | "dark";

/** The order the theme button steps through. */
export const THEME_CHOICES: readonly ThemeChoice[] = ["system", "light", "dark"];

function isChoice(value: unknown): value is ThemeChoice {
  return typeof value === "string" && (THEME_CHOICES as readonly string[]).includes(value);
}

/** The stored choice, or the system's when there is none or storage is refused. */
export function readTheme(): ThemeChoice {
  try {
    const stored = localStorage.getItem(THEME_STORAGE_KEY);
    return isChoice(stored) ? stored : "system";
  } catch {
    return "system";
  }
}

/** Sets data-theme on the document, which the stylesheet's dark block keys on; system removes it. */
export function applyTheme(choice: ThemeChoice): void {
  const root = document.documentElement;
  if (choice === "system") root.removeAttribute("data-theme");
  else root.setAttribute("data-theme", choice);
}

/** Applies a choice and remembers it for the next visit, when the browser allows. */
export function saveTheme(choice: ThemeChoice): void {
  applyTheme(choice);
  try {
    if (choice === "system") localStorage.removeItem(THEME_STORAGE_KEY);
    else localStorage.setItem(THEME_STORAGE_KEY, choice);
  } catch {
    // A browser that refuses storage still shows the choice until the page is left.
  }
}

/** The choice after this one, round the cycle. */
export function nextTheme(choice: ThemeChoice): ThemeChoice {
  const i = THEME_CHOICES.indexOf(choice);
  return THEME_CHOICES[(i + 1) % THEME_CHOICES.length] ?? "system";
}
