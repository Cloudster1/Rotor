import { useQuery } from "@tanstack/react-query";
import { type ReactNode, useId, useState } from "react";
import { type Person, peopleQuery } from "@/api/groups";
import { SEARCH_DELAY_MS } from "@/config";
import { input } from "@/features/settings/styles";
import { useDebounced } from "@/lib/debounce";
import { s } from "@/strings";
import { select } from "./ui";

/** An option already chosen: what it stands for and how it reads. */
interface Chosen {
  id: string;
  name: string;
}

const byId = (person: Person) => person.id;
const byName = (person: Person) => person.name;

/** Chooses one person by asking the server as somebody types, so it never needs everybody. Ported from Armature. */
export function PersonPicker({
  label,
  value,
  onChange,
  none,
  exclude = [],
  optionValue = byId,
  group,
  children,
  disabled,
  ...data
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  none?: string;
  exclude?: string[];
  optionValue?: (person: Person) => string;
  group?: string;
  children?: ReactNode;
  disabled?: boolean;
} & Record<`data-${string}`, string>) {
  const id = useId();
  const [words, setWords] = useState("");
  // Who was picked here, so they stay chosen after the search moves past them.
  const [picked, setPicked] = useState<Chosen | null>(null);
  const settled = useDebounced(words.trim(), SEARCH_DELAY_MS);
  const found = useQuery(peopleQuery(settled));
  const people = (found.data ?? []).filter((person) => !exclude.includes(person.id));
  const options = people.map((person) => ({ id: optionValue(person), name: byName(person) }));
  const kept = picked && value && picked.id === value && !options.some((option) => option.id === value) ? [picked] : [];
  const nothing = Boolean(settled) && found.data !== undefined && people.length === 0;

  function choose(next: string) {
    const option = options.find((each) => each.id === next);
    if (option) setPicked(option);
    onChange(next);
  }

  const list = [...kept, ...options].map((option) => (
    <option key={option.id} value={option.id}>
      {option.name}
    </option>
  ));
  return (
    <div className="flex min-w-0 flex-col gap-1" data-person-picker="">
      <label htmlFor={`${id}-select`} className="text-sm font-medium text-ink">
        {label}
      </label>
      <input
        type="search"
        className={input}
        aria-label={s.access.people.findFor(label)}
        placeholder={s.access.people.findPlaceholder}
        value={words}
        disabled={disabled}
        onChange={(event) => setWords(event.target.value)}
        // Enter in the search narrows the list; it must not submit the form around it.
        onKeyDown={(event) => event.key === "Enter" && event.preventDefault()}
        data-person-search=""
      />
      <select
        id={`${id}-select`}
        className={select}
        value={value}
        disabled={disabled}
        aria-describedby={nothing ? `${id}-nobody` : undefined}
        onChange={(event) => choose(event.target.value)}
        {...data}
      >
        {none !== undefined && <option value="">{none}</option>}
        {children}
        {group && list.length > 0 ? <optgroup label={group}>{list}</optgroup> : list}
      </select>
      {nothing && (
        <p id={`${id}-nobody`} className="text-xs text-ink-muted">
          {s.access.people.nobody}
        </p>
      )}
    </div>
  );
}
