import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type RefObject, useId, useState } from "react";
import { pickableSkillsQuery, skillVersionsQuery } from "@/api/skills";
import { link } from "@/features/settings/styles";
import { s } from "@/strings";
import { addSkillAt, type SkillAdded } from "./canvas/skills";
import type { YamlEditorHandle } from "./YamlEditor";

const card = "rounded-overlay border border-border bg-surface p-4 shadow-1";
const input = "h-8 w-full rounded-control border border-border-strong bg-surface px-2 text-sm text-ink";
const secondary = "h-8 rounded-control border border-border-strong px-3 text-sm font-medium text-ink hover:bg-surface-raised disabled:opacity-60";

/** What the picker last did, said beside it: a pin written, or why it was not. */
type Said = { ok: true; text: string } | { ok: false; text: string };

function sayRefused(result: Exclude<SkillAdded, { ok: true }>): string {
  const t = s.skills.picker;
  if (result.why === "notAgent") return t.notAgent(result.node, result.type);
  return result.why === "outside" ? t.outside : t.unparsed;
}

/** Pins a skill the person reads, at the version they pick, to the agent node at the YAML's cursor (#223). */
export function SkillPicker({ editor }: { editor: RefObject<YamlEditorHandle | null> }) {
  const id = useId();
  const t = s.skills.picker;
  const skills = useQuery(pickableSkillsQuery);
  const [skillID, setSkillID] = useState("");
  const versions = useQuery({ ...skillVersionsQuery(skillID), enabled: skillID !== "" });
  const [picked, setPicked] = useState<number>();
  const [said, setSaid] = useState<Said>();
  const skill = skills.data?.skills.find((k) => k.id === skillID);
  // Newest first, so the first is the one a new pin should follow unless the person picks another.
  const version = picked ?? versions.data?.[0]?.number;

  function insert() {
    const at = editor.current?.cursor();
    if (!skill || version === undefined || !at) return;
    let result: SkillAdded;
    try {
      result = addSkillAt(at.text, at.at, { name: skill.name, version });
    } catch {
      return setSaid({ ok: false, text: t.failed });
    }
    if (!result.ok) return setSaid({ ok: false, text: sayRefused(result) });
    editor.current?.apply(result.text);
    setSaid({ ok: true, text: (result.repinned ? t.repinned : t.added)(skill.name, version, result.node) });
  }

  const list = skills.data?.skills ?? [];
  return (
    <section className={card} aria-labelledby={`${id}-title`} data-skill-picker>
      <h2 id={`${id}-title`} className="mb-1 text-lg font-semibold text-ink">
        {t.title}
      </h2>
      <p className="mb-3 text-sm text-ink-muted">{t.body}</p>
      {skills.isPending && <p className="text-sm text-ink-muted">{t.loading}</p>}
      {skills.isError && (
        <p role="alert" className="text-sm text-danger">
          {skills.error.message}
        </p>
      )}
      {skills.data && list.length === 0 && (
        <p className="text-sm text-ink-muted">
          {t.none}{" "}
          <Link to="/skills" className={link}>
            {t.browse}
          </Link>
        </p>
      )}
      {list.length > 0 && (
        <div className="flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm text-ink">
            {t.skill}
            <select
              className={input}
              value={skillID}
              onChange={(e) => {
                setSkillID(e.target.value);
                setPicked(undefined);
                setSaid(undefined);
              }}
              data-picker-skill
            >
              <option value="">{t.choose}</option>
              {list.map((k) => (
                <option key={k.id} value={k.id} title={k.description}>
                  {k.builtin ? t.builtin(k.name) : k.name}
                </option>
              ))}
            </select>
          </label>
          {skill && <p className="text-xs text-ink-muted">{skill.description}</p>}
          {skill && versions.data && (
            <label className="flex flex-col gap-1 text-sm text-ink">
              {t.version}
              <select className={input} value={version ?? ""} onChange={(e) => setPicked(Number(e.target.value))} data-picker-version>
                {versions.data.map((v) => (
                  <option key={v.number} value={v.number}>
                    {t.versionOf(v.number, v.comment)}
                  </option>
                ))}
              </select>
            </label>
          )}
          <div>
            <button type="button" className={secondary} disabled={!skill || version === undefined} onClick={insert} data-action="insert-skill">
              {t.insert}
            </button>
          </div>
        </div>
      )}
      {/* Present from the start, so a screen reader hears what each insert did. */}
      <p
        role="status"
        className={`mt-2 text-sm empty:hidden ${said?.ok === false ? "text-danger" : "text-ink"}`}
        data-picker-said={said ? String(said.ok) : ""}
      >
        {said?.text ?? ""}
      </p>
    </section>
  );
}
