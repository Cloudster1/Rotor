import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { importSkill, skillsQueryKey } from "@/api/skills";
import { MAX_SKILL_IMPORT_BYTES, MAX_VERSION_COMMENT } from "@/config";
import { input, link, primary } from "@/features/settings/styles";
import { PageIntro } from "@/features/settings/ui";
import { Refusal } from "@/features/shell/Dialog";
import { PageHeader } from "@/features/shell/EmptyState";
import { s } from "@/strings";
import { ProblemList, problemsOf } from "./checks";
import { fieldLabel } from "./styles";

/** Imports a zipped skill folder (#220): a new skill, or the next version of the one of that name the person edits. */
export function ImportSkillPage() {
  const client = useQueryClient();
  const t = s.skills.import;
  const [file, setFile] = useState<File>();
  const [comment, setComment] = useState("");
  const [problem, setProblem] = useState("");
  const send = useMutation({
    mutationFn: (zip: File) => importSkill(zip, comment),
    onSuccess: () => client.invalidateQueries({ queryKey: skillsQueryKey }),
  });
  const refused = problemsOf(send.error);
  const done = send.data;

  function submit() {
    setProblem("");
    send.reset();
    if (!file) return setProblem(t.chooseFile);
    if (file.size > MAX_SKILL_IMPORT_BYTES) return setProblem(t.tooLarge);
    send.mutate(file);
  }

  return (
    <div className="flex max-w-3xl flex-col gap-4">
      <PageHeader title={t.title} />
      <PageIntro>{t.intro}</PageIntro>
      <form
        className="flex flex-col gap-4"
        noValidate
        aria-label={t.title}
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        <label className={fieldLabel}>
          {t.file}
          <input
            type="file"
            accept=".zip,application/zip"
            className="text-sm text-ink file:mr-3 file:h-9 file:rounded-control file:border file:border-border-strong file:bg-surface file:px-3 file:text-sm file:font-medium file:text-ink"
            onChange={(e) => setFile(e.target.files?.[0])}
            data-import-zip
          />
        </label>
        <label className={fieldLabel}>
          {t.comment}
          <input className={input} value={comment} onChange={(e) => setComment(e.target.value)} maxLength={MAX_VERSION_COMMENT} name="comment" />
        </label>
        <div className="flex flex-wrap items-center gap-4">
          <button type="submit" className={primary} disabled={send.isPending} data-action="import-skill">
            {send.isPending ? t.importing : t.import}
          </button>
          <Link to="/skills/new" className={link}>
            {t.newInstead}
          </Link>
        </div>
      </form>
      {/* Present from the start, so a screen reader hears which happened. */}
      <div
        role="status"
        className="flex flex-col items-start gap-2 text-sm text-ink empty:hidden"
        data-import-result={done ? (done.created ? "created" : "added") : ""}
      >
        {done && (
          <>
            <p>{done.created ? t.created(done.skill.name) : t.added(done.skill.name, done.version.number)}</p>
            <Link to="/skills/$skillID/files" params={{ skillID: done.skill.id }} className={link} data-action="open-imported">
              {t.open(done.skill.name)}
            </Link>
          </>
        )}
      </div>
      {problem && (
        <p role="alert" className="text-sm text-danger">
          {problem}
        </p>
      )}
      {refused.length > 0 && (
        <div role="alert" className="flex flex-col gap-2">
          <p className="text-sm font-medium text-danger">{t.refused}</p>
          <ProblemList problems={refused} data-import-problems="" />
        </div>
      )}
      {refused.length === 0 && <Refusal error={send.error} />}
    </div>
  );
}
