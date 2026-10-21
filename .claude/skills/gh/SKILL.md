---
name: gh
description: GitHub workflow for this repo, read before any code change. Creating issues, issue branches, running tests, opening PRs. Use this whenever you want to change code, commit, push or open a pull request.
---

# GitHub workflow

Repo: `Cloudster1/Rotor`, default branch: `main`

## Rules

- Every change needs an issue and a branch named `<type>/<issue>-<slug>`.
  Types: `feat fix chore docs refactor test perf ci build revert`.
- Never push to `main`. Merging needs the user's approval; the guard asks for it on `gh pr merge`.
- Agree on issues and PRs with the user before creating them.
- Always call `gh` non-interactively: `--title` / `--body-file`, never open the editor.
- `.claude/hooks/branch-guard.sh` enforces this; a violation is blocked, not commented on.
- Issues carry a milestone (`M0 Foundation`, `M1 Tool connections`, `M2 Workflow engine`,
  `M3 First run (MVP)`, `M4 Sandboxes on Kubernetes`, `M5 Approvals`, `M6 Triggers`,
  `M7 Observability and audit`, `M8 Security hardening`, `M9 Suite and release`, `M10 Assistants and editor`,
  `M11 OpenShell sandboxes` or `Backlog`),
  an `area/*` label, a `type/*` label and `mvp` or `backlog`. `blocked` marks an issue waiting on
  work outside this repository.
- Areas: `backend`, `web`, `auth`, `integration`, `infra`, `runner`, `workflows`, `connections`,
  `sandbox`, `observability`, `security`.
- Issue bodies follow the feature template: User story, Acceptance criteria, Tests, Implementation,
  Depends on.

## Language

Everything in the repository is written in English: issues, pull requests, commit messages, code,
comments and documentation. User-facing strings live in `web/src/strings.ts`.

## Exciter, Armature and Stator

Changes Rotor needs in a sibling go through issues and pull requests in `Cloudster1/Exciter`,
`Cloudster1/Armature` and `praetorianer777/Stator`; the suite itself lives in
`praetorianer777/Dynamo`. Link them from the Rotor issue and label the Rotor issue `blocked` until
they ship.

## Recipe

```bash
gh auth status                       # preflight

gh issue list --limit 20             # does the issue already exist?
gh issue create --title "..." --body-file /tmp/body.md --label area/backend --label type/feature --label mvp --milestone "M1 Tool connections"

gh issue develop 42 --name feat/42-token-minting --base main --checkout

# ... work ...

./run-tests.sh                       # everything; the hook runs the quick subset before every push (ROTOR_GATE=full for all)
git push -u origin HEAD

gh pr create --title "..." --body-file /tmp/pr.md   # body contains "Closes #42"
gh pr checks --watch
gh run view --log-failed             # when CI is red
```

## Pitfalls

- `gh issue develop` creates the branch on the remote and checks it out; no manual
  `git switch -c` needed.
- Branch slugs are lowercase ASCII: `feat/42-token-minting`, not `feat/42-Token-Minting`.
- Commit messages are Conventional Commits, lowercase, imperative, subject at most 72 characters, and
  describe the **effect** rather than the files touched.
- A push with `run_in_background` spends the whole gate before it is reported as started, then
  finishes in seconds; that is the gate having passed, not skipped. Every guard decision and gate
  duration is in `$(git rev-parse --git-common-dir)/branch-guard.log`.
- Parallel work happens in worktrees under `.claude/worktrees/`; each gets its own compose
  project and ports from `run-tests.sh`, so gates never collide.
