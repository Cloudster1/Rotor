# Contributing to Rotor

Thank you for helping. Rotor is the agentic backplane of the suite: people
create, start and watch workflows that read and write
[Exciter](https://github.com/Cloudster1/Exciter) (requirements),
[Armature](https://github.com/Cloudster1/Armature) (issues and service desk)
and [Stator](https://github.com/praetorianer777/Stator) (the wiki) through
their MCP servers, always with the permissions of the person who started
them. Rotor mirrors the siblings' stack and patterns on purpose: when in doubt,
look at how Exciter does it, then Armature and Stator, and do the same.
[`docs/architecture.md`](docs/architecture.md) describes how Rotor is built and
[`docs/decisions.md`](docs/decisions.md) why.

Report a vulnerability privately, as [`SECURITY.md`](SECURITY.md) says, never
in a public issue.

## Getting started

Every toolchain runs in a container, driven by the `Makefile`. The host needs
Docker, make, `jq` and `python3`; `gh` opens pull requests. Do not install Go,
Python, Node, Chromium or a Postgres client on the host: if a tool is missing,
add a make target that runs it in a container.

```sh
make up                 # the stack, and where each part of it listens
./run-tests.sh          # the whole gate, as CI runs it
make help               # every target
```

`SECURITY.md` lists the development accounts and secrets the stack ships with.

## Every change hangs off an issue

1. Find or open an issue. Feature issues follow the template: user story,
   acceptance criteria, tests, implementation, depends on. Changes Rotor
   needs in a sibling are issues there, linked from the Rotor issue, which is
   labelled `blocked` until they ship.
2. Work on a branch named `<type>/<issue>-<slug>`, lowercase ASCII, for
   example `feat/12-token-minting`. The types are `feat fix chore docs
   refactor test perf ci build revert`.
3. Commit messages are [Conventional Commits](https://www.conventionalcommits.org/):
   lowercase, imperative, a subject of at most 72 characters that says what
   changes for the user rather than which files were touched.
4. Say what changes for the user in `CHANGELOG.md`, under Unreleased.
5. Open a pull request whose description carries `Closes #N`. CI runs every
   layer of the gate and must pass before it is merged.

## Testing

`./run-tests.sh` runs every layer; `./run-tests.sh --list` names them:

- `shell`: the tests of the tooling itself, the stack's definition and the realm
- `check-format`: Biome over `web/` and `e2e/`
- `check-go`: gofmt, vet, Go unit tests with the race detector, OpenAPI drift
- `check-python`: ruff, mypy, pytest and a smoke run of the runner image
- `check-web`: eslint, TypeScript, vitest and API type drift
- `test-integration`: Go integration tests against the compose stack (`-tags integration`)
- `test-e2e`: Playwright in the browser, with an accessibility check on every page

A feature is finished when every layer passes, not when the code is written.
A new layer joins the gate in the change that introduces it, through a make
target of the same name; until then `run-tests.sh` skips it.

Nothing is mocked below the layer under test. A guard is not proven by the
service refusing: try the same thing straight through SQL as `rotor_app` and
watch the database refuse it too.

## The API

`api/openapi.json` is derived from the route table. After changing a route, a
request type or a response type, run `make openapi` and commit the regenerated
document and `web/src/api/schema.d.ts`; a unit test refuses a stale copy. An
operation agreed before it is built carries `pending: true` and answers 501;
`docs/api-contract-m*.md` hold the agreements.

## Style

- Comments say why, never what. Doc comments are at most two lines.
- No en dashes, em dashes, typographic quotes or emoji, in code or in prose.
- Named constants over magic numbers.
- No source file grows past 500 lines; split it by what its parts do.
- Errors the user reads are sentences, and say what to do about it.
- User-facing strings live in `web/src/strings.ts`; everything else, from code
  to issues, is written in English.
- Code adapted from a sibling keeps working like theirs, and
  [`NOTICE`](NOTICE) credits it with the commit it came from.
- Secrets never land in the repository, the database, a log line or a
  workflow's events: long-lived ones live in OpenBao, short-lived ones in
  memory.

## Working with Claude Code

The repository carries a [Claude Code](https://claude.com/claude-code) setup:
[`CLAUDE.md`](CLAUDE.md), `.claude/skills/gh` and the hooks in
`.claude/settings.json`. When the repository is opened in Claude Code, its
hooks refuse edits on a branch that is not named `<type>/<issue>-<slug>` and
run the layers of the gate a push can affect before every push. Nothing of it
runs outside Claude Code.
