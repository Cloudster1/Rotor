import { useId } from "react";
import { s } from "@/strings";
import { BLANK_STARTER, SHIPPED_STARTERS, type Starter } from "./starters";

const card = "rounded-overlay border border-border bg-surface px-4 py-3 shadow-1";

/** Offers the blank template and every workflow Rotor ships as the text a new workflow begins with. */
export function StarterPicker({ selected, onPick }: { selected: string; onPick: (starter: Starter) => void }) {
  const id = useId();
  const group = `${id}-start`;
  const entries = [{ ...BLANK_STARTER, description: s.workflows.starters.blankBody }, ...SHIPPED_STARTERS];
  const label = (name: string) => (name === "" ? s.workflows.starters.blank : name);
  return (
    <details className={card} data-starter-picker>
      <summary className="cursor-pointer text-sm text-ink">
        <span className="font-semibold">{s.workflows.starters.title}</span>: {label(selected)}
      </summary>
      <fieldset className="mt-3">
        <legend className="sr-only">{s.workflows.starters.title}</legend>
        <p className="mb-3 text-sm text-ink-muted">{s.workflows.starters.body}</p>
        <ul className="flex flex-col gap-3">
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
                  <span className="block font-medium">{label(entry.name)}</span>
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
    </details>
  );
}
