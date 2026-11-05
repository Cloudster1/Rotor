import { useId } from "react";
import { s } from "@/strings";
import type { SpecNode } from "./specDoc";
import { secondary } from "./styles";

/** The skills an agent names, each with its pinned version; one comes off here, and the editor's picker adds them (#223). */
export function NodeSkills({ node, readOnly, onRemove }: { node: SpecNode; readOnly: boolean; onRemove: (index: number) => void }) {
  const id = useId();
  const t = s.skills.inspector;
  const skills = node.skills ?? [];
  return (
    <div className="flex flex-col gap-1" data-node-skills>
      <p id={`${id}-title`} className="text-sm font-medium text-ink">
        {t.title}
      </p>
      {skills.length === 0 ? (
        <p className="text-xs text-ink-muted">{t.none}</p>
      ) : (
        <ul className="flex flex-col gap-1" aria-labelledby={`${id}-title`}>
          {skills.map((skill, index) => (
            <li key={`${skill.name}-${skill.version}`} className="flex flex-wrap items-center justify-between gap-2 text-sm" data-node-skill={skill.name}>
              <span className="font-mono text-ink">{t.pin(skill.name, skill.version)}</span>
              {!readOnly && (
                <button type="button" className={secondary} onClick={() => onRemove(index)} aria-label={t.removeNamed(skill.name)} data-action="remove-skill">
                  {t.remove}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
