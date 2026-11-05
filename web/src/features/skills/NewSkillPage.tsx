import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useId, useState } from "react";
import { createSkill, skillsQueryKey } from "@/api/skills";
import { MAX_VERSION_COMMENT, SKILL_MD } from "@/config";
import { input, link, primary } from "@/features/settings/styles";
import { PageIntro } from "@/features/settings/ui";
import { Refusal } from "@/features/shell/Dialog";
import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";
import { ProblemList, problemsOf, useSkillCheck } from "./checks";
import { area, fieldLabel } from "./styles";
import { NEW_SKILL_MD } from "./template";

const SKILL_ROWS = 18;

/** A new skill from a SKILL.md that passes the checks, checked again as it is typed; more files come once it is made. */
export function NewSkillPage() {
  const id = useId();
  const client = useQueryClient();
  const navigate = useNavigate();
  const t = s.skills.create;
  const [text, setText] = useState(NEW_SKILL_MD);
  const [comment, setComment] = useState("");
  const files = [{ path: SKILL_MD, content: text }];
  const check = useSkillCheck(files);
  const create = useMutation({
    mutationFn: () => createSkill(files, comment),
    onSuccess: async (made) => {
      await client.invalidateQueries({ queryKey: skillsQueryKey });
      await navigate({ to: "/skills/$skillID/files", params: { skillID: made.skill.id } });
    },
  });
  const refused = problemsOf(create.error);
  const problems = refused.length > 0 ? refused : check.problems;
  return (
    <div className="flex max-w-4xl flex-col gap-4">
      <PageHeader title={t.title} />
      <PageIntro>{t.intro}</PageIntro>
      <form
        className="flex flex-col gap-4"
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          create.mutate();
        }}
        aria-label={t.title}
      >
        <div className="flex flex-col gap-1">
          <label className={fieldLabel}>
            {t.label}
            <textarea
              className={area}
              rows={SKILL_ROWS}
              value={text}
              onChange={(e) => {
                setText(e.target.value);
                if (create.isError) create.reset();
              }}
              aria-describedby={`${id}-hint`}
              aria-invalid={problems.length > 0}
              spellCheck={false}
              data-new-skill-md
            />
          </label>
          <p id={`${id}-hint`} className="text-xs text-ink-muted">
            {t.hint}
          </p>
        </div>
        <div aria-live="polite">
          <ProblemList problems={problems} checking={check.checking} data-skill-problems="" />
        </div>
        <label className={fieldLabel}>
          {t.comment}
          <input className={input} value={comment} onChange={(e) => setComment(e.target.value)} maxLength={MAX_VERSION_COMMENT} name="comment" />
        </label>
        <div className="flex flex-wrap items-center gap-4">
          <button type="submit" className={primary} disabled={create.isPending} data-action="create-skill">
            {create.isPending ? t.creating : t.create}
          </button>
          <Link to="/skills/import" className={link}>
            {t.importInstead}
          </Link>
        </div>
        {refused.length === 0 && <Refusal error={create.error} />}
      </form>
    </div>
  );
}
