import { useEffect, useState } from "react";
import { ApiError } from "@/api/client";
import { type SkillFileBody, type SkillProblem, validateSkill } from "@/api/skills";
import { HTTP_UNPROCESSABLE, SKILL_NO_PATH, VALIDATE_DELAY_MS } from "@/config";
import { s } from "@/strings";

/** What the library says is wrong with files as they are typed, asked once typing pauses, as the workflow editor does. */
export function useSkillCheck(files: SkillFileBody[], enabled = true): { problems: SkillProblem[]; checking: boolean } {
  const [problems, setProblems] = useState<SkillProblem[]>([]);
  const [checking, setChecking] = useState(false);
  // The files are compared by what they say, so a new array of the same files asks nothing.
  const said = JSON.stringify(files);
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    setChecking(true);
    const timer = setTimeout(async () => {
      try {
        const found = await validateSkill(JSON.parse(said) as SkillFileBody[]);
        if (!cancelled) setProblems(found);
      } catch {
        // A check that fails says nothing; the save says what is wrong for certain.
      } finally {
        if (!cancelled) setChecking(false);
      }
    }, VALIDATE_DELAY_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [said, enabled]);
  return { problems, checking };
}

/** The problems a refused save or import named, or none when it was refused for something else. */
export function problemsOf(error: unknown): SkillProblem[] {
  return error instanceof ApiError && error.status === HTTP_UNPROCESSABLE ? error.problems : [];
}

/** Problems, each under the file it is in; those of the skill as a whole come first. */
export function ProblemList({ problems, checking, ...data }: { problems: SkillProblem[]; checking?: boolean } & Record<`data-${string}`, string>) {
  const t = s.skills.files;
  if (checking && problems.length === 0) return <p className="text-sm text-ink-muted">{t.checking}</p>;
  if (problems.length === 0) {
    return checking === undefined ? null : (
      <p className="text-sm text-ink-muted" data-skill-clean="">
        {t.clean}
      </p>
    );
  }
  const paths = [...new Set(problems.map((p) => p.path || SKILL_NO_PATH))].sort((a, b) => (a === SKILL_NO_PATH ? -1 : b === SKILL_NO_PATH ? 1 : 0));
  return (
    <div className="flex flex-col gap-2 rounded-control border border-danger bg-danger-subtle px-3 py-2" {...data}>
      {paths.map((path) => (
        <div key={path} data-problems-at={path}>
          <p className="text-sm font-medium text-ink">{path === SKILL_NO_PATH ? t.general : t.at(path)}</p>
          <ul className="ml-4 list-disc">
            {problems
              .filter((p) => (p.path || SKILL_NO_PATH) === path)
              .map((p) => (
                <li key={`${p.code}-${p.message}`} className="text-sm text-danger" data-problem={p.code}>
                  {p.message}
                </li>
              ))}
          </ul>
        </div>
      ))}
    </div>
  );
}
