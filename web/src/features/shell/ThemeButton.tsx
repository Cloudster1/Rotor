import { useState } from "react";
import { nextTheme, readTheme, saveTheme, type ThemeChoice } from "@/lib/theme";
import { s } from "@/strings";
import { Icon } from "./icons";

const LABELS: Record<ThemeChoice, string> = { system: s.theme.system, light: s.theme.light, dark: s.theme.dark };
const GLYPHS = { system: Icon.Monitor, light: Icon.Sun, dark: Icon.Moon } as const;

/** Steps through following the system, light and dark; the label names the scheme in force. */
export function ThemeButton() {
  const [choice, setChoice] = useState<ThemeChoice>(readTheme);
  const Glyph = GLYPHS[choice];
  return (
    <button
      type="button"
      onClick={() => {
        const next = nextTheme(choice);
        saveTheme(next);
        setChoice(next);
      }}
      aria-label={LABELS[choice]}
      title={LABELS[choice]}
      data-action="theme"
      data-theme-choice={choice}
      className="flex size-8 items-center justify-center rounded-control text-ink-muted hover:bg-surface-raised hover:text-ink"
    >
      <Glyph />
    </button>
  );
}
