import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useId, useState } from "react";
import { ApiError } from "@/api/client";
import { type SkillFileBody, type SkillSummary, saveSkill, skillsQueryKey } from "@/api/skills";
import { HTTP_CONFLICT, MAX_VERSION_COMMENT, SKILL_MD } from "@/config";
import { Refusal } from "@/features/shell/Dialog";
import { input, primary } from "@/features/settings/styles";
import { s } from "@/strings";
import { ProblemList, problemsOf, useSkillCheck } from "./checks";
import { ordered } from "./shared";
import { area, fieldLabel, secondary } from "./styles";

const FILE_ROWS = 20;

/** The current version's files, changed in place and saved together as the next version with a comment. */
export function FileEditor({
  skill,
  initial,
  onCancel,
  onSaved,
}: {
  skill: SkillSummary;
  initial: SkillFileBody[];
  onCancel: () => void;
  onSaved: (n: number) => void;
}) {
  const id = useId();
  const client = useQueryClient();
  const t = s.skills.files;
  const [draft, setDraft] = useState<SkillFileBody[]>(initial);
  const [selected, setSelected] = useState(SKILL_MD);
  const [newPath, setNewPath] = useState("");
  const [pathProblem, setPathProblem] = useState("");
  const [said, setSaid] = useState("");
  const [comment, setComment] = useState("");
  const check = useSkillCheck(draft);
  const save = useMutation({
    mutationFn: () => saveSkill(skill.id, draft, comment, skill.currentVersion),
    onSuccess: async (saved) => {
      await client.invalidateQueries({ queryKey: skillsQueryKey });
      onSaved(saved.version.number);
    },
  });
  const stale = save.error instanceof ApiError && save.error.status === HTTP_CONFLICT && save.error.code === "skill_changed";
  const refused = problemsOf(save.error);
  // What the save refused says it for certain; until a save, the live check speaks.
  const problems = refused.length > 0 ? refused : check.problems;
  const shown = draft.find((f) => f.path === selected) ?? draft[0];
  const shownProblems = problems.filter((p) => p.path === shown?.path);
  const files = ordered(draft);

  // A change answers what the last save refused for, so the live check speaks again.
  function change(next: SkillFileBody[]) {
    setDraft(next);
    if (save.isError) save.reset();
  }

  function add(event: FormEvent) {
    event.preventDefault();
    const path = newPath.trim();
    if (path === "") return setPathProblem(t.pathNeeded);
    if (draft.some((f) => f.path === path)) return setPathProblem(t.pathTaken(path));
    setPathProblem("");
    change([...draft, { path, content: "" }]);
    setSelected(path);
    setNewPath("");
  }

  function remove(path: string) {
    change(draft.filter((f) => f.path !== path));
    if (selected === path) setSelected(SKILL_MD);
    setSaid(t.removed(path));
  }

  async function reload() {
    await client.invalidateQueries({ queryKey: skillsQueryKey });
    onCancel();
  }

  return (
    <div className="flex flex-col gap-4" data-skill-editor="">
      <div className="grid gap-4 lg:grid-cols-[16rem_minmax(0,1fr)]">
        <div className="flex flex-col gap-3">
          <nav aria-label={t.list}>
            <ul className="flex flex-col gap-1">
              {files.map((f) => {
                const count = problems.filter((p) => p.path === f.path).length;
                return (
                  <li key={f.path} className="flex items-center gap-1">
                    <button
                      type="button"
                      className={`min-w-0 flex-1 rounded-control px-2 py-1 text-left font-mono text-sm break-all ${f.path === shown?.path ? "bg-accent-subtle text-accent" : "text-ink hover:bg-surface-raised"}`}
                      aria-current={f.path === shown?.path ? "true" : undefined}
                      onClick={() => setSelected(f.path)}
                      data-skill-file={f.path}
                    >
                      {f.path}
                      {count > 0 && <span className="ml-1 font-sans text-xs text-danger">({t.problemsIn(count)})</span>}
                    </button>
                    {f.path !== SKILL_MD && (
                      <button type="button" className={secondary} onClick={() => remove(f.path)} aria-label={t.removeNamed(f.path)} data-action="remove-file">
                        {t.remove}
                      </button>
                    )}
                  </li>
                );
              })}
            </ul>
          </nav>
          <form className="flex flex-col gap-2 border-t border-border pt-3" onSubmit={add} aria-label={t.add} noValidate>
            <label className={fieldLabel}>
              {t.addPath}
              <input
                className={`${input} font-mono`}
                value={newPath}
                onChange={(e) => setNewPath(e.target.value)}
                aria-describedby={`${id}-path-hint`}
                aria-invalid={pathProblem !== ""}
                data-new-file-path
              />
            </label>
            <p id={`${id}-path-hint`} className="text-xs text-ink-muted">
              {t.addHint}
            </p>
            {pathProblem && (
              <p role="alert" className="text-sm text-danger">
                {pathProblem}
              </p>
            )}
            <div>
              <button type="submit" className={secondary} data-action="add-file">
                {t.addButton}
              </button>
            </div>
          </form>
          <p role="status" className="text-xs text-ink-muted">
            {said}
          </p>
        </div>
        {shown && (
          <div className="flex min-w-0 flex-col gap-2">
            <label className={fieldLabel}>
              {t.content(shown.path)}
              <textarea
                key={shown.path}
                className={area}
                rows={FILE_ROWS}
                value={shown.content}
                onChange={(e) => change(draft.map((f) => (f.path === shown.path ? { ...f, content: e.target.value } : f)))}
                aria-invalid={shownProblems.length > 0}
                aria-describedby={shownProblems.length > 0 ? `${id}-file-problems` : undefined}
                spellCheck={false}
                data-skill-textarea={shown.path}
              />
            </label>
            {shownProblems.length > 0 && (
              <ul id={`${id}-file-problems`} className="flex flex-col gap-1" data-file-problems={shown.path}>
                {shownProblems.map((p) => (
                  <li key={`${p.code}-${p.message}`} className="text-sm text-danger">
                    {p.message}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
      <div aria-live="polite">
        <ProblemList problems={problems} checking={check.checking} data-skill-problems="" />
      </div>
      <label className={`${fieldLabel} max-w-3xl`}>
        {t.comment}
        <input className={input} value={comment} onChange={(e) => setComment(e.target.value)} maxLength={MAX_VERSION_COMMENT} name="comment" />
      </label>
      <div className="flex flex-wrap items-center gap-3">
        <button type="button" className={primary} disabled={save.isPending} onClick={() => save.mutate()} data-action="save-skill">
          {save.isPending ? t.saving : t.save(skill.currentVersion + 1)}
        </button>
        <button type="button" className={secondary} onClick={onCancel} data-action="cancel-edit">
          {t.cancel}
        </button>
      </div>
      {stale && (
        <div role="alert" className="flex flex-col items-start gap-2 rounded-control border border-danger bg-danger-subtle px-3 py-2" data-skill-stale="">
          <p className="text-sm text-danger">{t.stale}</p>
          <button type="button" className={secondary} onClick={reload} data-action="reload-skill">
            {t.reload}
          </button>
        </div>
      )}
      {!stale && refused.length === 0 && <Refusal error={save.error} />}
    </div>
  );
}
