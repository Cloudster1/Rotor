import { useId } from "react";
import { s } from "@/strings";
import { BLANK_STARTER, SHIPPED_STARTERS, type Starter } from "./starters";

const card = "rounded-overlay border border-border bg-surface p-4 shadow-1";

/** Offers the blank template and every workflow Rotor ships as the text a new workflow begins with. */
export function StarterPicker({ selected, onPick }: { selected: string; onPick: (starter: Starter) => void }) {
  const id = useId();
  const group = `${id}-start`;
  const entries = [{ ...BLANK_STARTER, description: s.workflows.starters.blankBody }, ...SHIPPED_STARTERS];
  return (
    <fieldset className={card} data-starter-picker>
      <legend className="px-1 text-lg font-semibold text-ink">{s.workflows.starters.title}</legend>
      <p className="mb-3 text-sm text-ink-muted">{s.workflows.starters.body}</p>
      <ul className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {entries.map((entry) => (
          <li key={entry.name || "blank"} className="flex flex-col gap-1">
            <label className="flex items-start gap-2 text-sm text-ink">
              <input
                type="radio"
                className="mt-1"
                name={group}
                checked={entry.name === selected}
                onChange={() => onPick(entry)}
                data-starter={entry.name || "blank"}
              />
              <span>
                <span className="block font-medium">{entry.name === "" ? s.workflows.starters.blank : entry.name}</span>
                <span className="block text-ink-muted">{entry.description}</span>
              </span>
            </label>
            {entry.docs && (
              <a
                href={entry.docs}
                target="_blank"
                rel="noreferrer"
                className="ml-6 text-sm font-medium text-ink underline hover:text-accent"
                aria-label={s.workflows.starters.docsNamed(entry.name)}
              >
                {s.workflows.starters.docs}
              </a>
            )}
          </li>
        ))}
      </ul>
    </fieldset>
  );
}
