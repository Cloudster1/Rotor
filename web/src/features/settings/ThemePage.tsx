import { useId } from "react";
import { saveTheme, THEME_CHOICES } from "@/lib/theme";
import { useTheme } from "@/lib/useTheme";
import { s } from "@/strings";
import { SettingsCard, SettingsPage } from "./ui";

/** The colour scheme as a choice of three, the same one the top bar's button steps through (#206). */
export function ThemePage() {
  const id = useId();
  const choice = useTheme();
  return (
    <SettingsPage title={s.settings.pages.theme} intro={s.settings.intros.theme}>
      <SettingsCard titleId="theme-title" title={s.themePage.title} description={s.themePage.body} descriptionId={`${id}-body`} data-theme-page>
        <fieldset className="flex flex-col gap-2" aria-describedby={`${id}-body`}>
          <legend className="sr-only">{s.themePage.title}</legend>
          {THEME_CHOICES.map((each) => (
            <label
              key={each}
              className="flex cursor-pointer items-start gap-3 rounded-control border border-border p-3 has-[:checked]:border-accent has-[:checked]:bg-accent-subtle"
            >
              <input
                type="radio"
                name={`${id}-theme`}
                value={each}
                checked={choice === each}
                onChange={() => saveTheme(each)}
                aria-labelledby={`${id}-${each}-name`}
                aria-describedby={`${id}-${each}`}
                className="mt-1 accent-accent"
                data-theme-option={each}
              />
              <span className="flex flex-col">
                <span id={`${id}-${each}-name`} className="text-sm font-medium text-ink">
                  {s.themePage.choices[each]}
                </span>
                <span id={`${id}-${each}`} className="text-xs text-ink-muted">
                  {s.themePage.hints[each]}
                </span>
              </span>
            </label>
          ))}
        </fieldset>
      </SettingsCard>
    </SettingsPage>
  );
}
