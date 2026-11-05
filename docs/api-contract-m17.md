# API contract for M17: skills

M17 gives workflow agents skills: folders in the
[Agent Skills](https://agentskills.io) format, a `SKILL.md` with a name, a
description and instructions, plus reference files. Rotor keeps them in a
library, versioned and shared like workflows; an agent node names the skills
it may use, each pinned to a version; and in a run the agent sees each skill's
name and description and loads the rest when it needs it.
- a library of skills: stored, versioned and shared (#219)
- importing and exporting a skill as a zip (#220)
- agent nodes that name their skills, checked on save and at run start (#221)
- loading a skill during a run (#222)
- the skills pages and the skill picker in the web app (#223)
- skills through Rotor's MCP endpoint (#224)
- the skills Rotor ships, used by the default workflows (#225)

This file is #218. The schema `api/workflow.v1.schema.json` is the source of
truth for the spec; this file says what it means. Until #221 merged, a
workflow whose agent names skills was refused when saved and when a run started,
with the problem `not_built`. The skill routes carry `pending: true` and
answer 501 until the issue that builds them merges.

Conventions are M2's: paths under `/api/v1`; refusals are sentences saying
what to do; a page of a listing takes `limit` (50, at most 200) and `offset`
and answers `total`.

## Migrations, packages and settings

- Migration ranges:

  | Range | Issue | What |
  |---|---|---|
  | 01400-01419 | #219 | `skill`, `skill_version`, `skill_file`; the `skill.*` permissions and roles; `role_assignment.skill_id`; `actor_can_skill()` and the policies |
  | 01420-01429 | #225 | anything the built-in skills need beyond #219, if any |

- Packages: `internal/skills` (the library, its checks, import and export, and
  the built-in skills), `internal/workflow/spec` and the runner's `spec.py`
  (the `skills` field), `internal/runnerctl` and `proto/rotor/runner/v1`
  (handing skills to a run), `runner/src/rotor_runner/skills.py` (the
  `read_skill` tool), `web/src/features/skills`.
- Limits, in `internal/config` and mirrored in the runner where it checks
  them:
  - `MaxSkillNameChars` = 64; a name is lowercase letters, digits and single
    dashes, starting and ending with a letter or digit, as Agent Skills asks.
  - `MaxSkillDescriptionChars` = 1024.
  - `MaxSkillFiles` = 50 files in a version, `SKILL.md` included.
  - `MaxSkillFileBytes` = 256 KiB a file.
  - `MaxSkillVersionBytes` = 1 MiB for all of a version's files.
  - `MaxSkillPathDepth` = 5 folders below the skill's root.
  - `MaxSkillImportBytes` = 2 MiB of zip, uncompressed size judged against
    `MaxSkillVersionBytes` before anything is unpacked.
  - `MaxAgentSkills` = 10 skills on one agent node.
  - `MaxSkillReadBytes` = 64 KiB that one `read_skill` call answers.

## The skill format (#219, #220)

A skill is the files of one folder. `SKILL.md` sits at its root:

```markdown
---
name: requirements-engineering
description: Requirements engineering method for eliciting, writing and reviewing requirements. Use whenever ...
license: Apache-2.0
metadata:
  version: "2"
---

# Requirements engineering

Turn stakeholder intent into requirements that are ...
```

- `name` and `description` are required. `name` follows the name rule above
  and is the skill's name in Rotor; it cannot change after the skill is made,
  so every version's `name` must be the skill's. `description` is plain text,
  1 to `MaxSkillDescriptionChars` characters, and says what the skill does and
  when to use it: an agent decides from it alone whether to load the rest.
- `license`, `compatibility`, `metadata` (strings to strings) and
  `allowed-tools` are kept as written and shown, but Rotor enforces none of
  them; what an agent may call is still its node's bindings.
- Other files sit under relative paths (`references/templates.md`), at most
  `MaxSkillPathDepth` folders deep, without `..`, a leading `/`, a backslash
  or a name starting with `.`. Every file is UTF-8 text without NUL bytes;
  binary files, and with them images and executables, are left for later.

## The library (#219)

### Tables

- `skill`: `id` (uuidv7), `name` (unique in the installation, as workflows
  name skills by it), `description` (the current version's), `owner_id` and
  `owner_name`, `current_version`, `archived_at`, `builtin`. A built-in skill
  has no owner (`CHECK (builtin OR owner_id IS NOT NULL)`).
- `skill_version`: `(skill_id, number)`, `front_matter jsonb`, `description`,
  `hash` (sha256 over the sorted paths and contents), `bytes`, `comment` (at
  most 500 characters), `created_by`, `created_by_name`, `created_at`.
  Numbered 1, 2, 3 by a trigger as `workflow_version` is, except that a
  built-in skill takes the number its `metadata.version` declares (#225).
  Immutable: `rotor_app` and `rotor_admin` hold no `UPDATE` or `DELETE`.
- `skill_file`: `(skill_id, number, path)`, `content text`, `bytes`, `sha256`.
  `SKILL.md` is a file like any other. Immutable with its version.

A skill is archived, never deleted, so a workflow version that pins it keeps
meaning what it meant.

### Access

Skills take the access model of workflows (`docs/api-contract-access.md`):

- Permissions `skill.read`, `skill.edit`, `skill.administer` and
  `skill.create`, added to `role_known_permissions` and `perm.AllPermissions`.
- Built-in roles: Skill administrator (`skill.read`, `skill.edit`,
  `skill.administer`), Skill editor (`skill.read`, `skill.edit`) and Skill
  reader (`skill.read`). Author gains `skill.create`; Administrator gains all
  four over the installation.
- `role_assignment.skill_id`: a grant on one skill. At most one of
  `workflow_id` and `skill_id` is set; neither means the installation.
- `actor_can_skill(permission, skill_id)` answers as `actor_can` does for a
  workflow. The creator of a skill is granted Skill administrator on it, as
  the creator of a workflow is.
- "Available to everyone" is a Skill reader grant to the Everyone group,
  given like any grant.
- A built-in skill is readable by everyone signed in. Nobody edits, archives
  or shares it through the API; only the api's start writes it (#225).

### Endpoints

"Session" means a browser session, never an assistant, as for workflow
grants.

| Method and path | Who | What |
|---|---|---|
| `GET /skills` | signed in | the skills the caller reads, built-in ones included; `archived`, `q` (name or description), `limit`, `offset` |
| `POST /skills` | `skill.create` | `{files: [{path, content}], comment}` makes the skill from its `SKILL.md` and answers it with version 1 |
| `POST /skills/validate` | signed in | checks `{files}` as a save would, without saving, and answers the problems |
| `GET /skills/{skillID}` | `skill.read` | the skill, what the caller may do with it (`canEdit`, `canAdminister`), and its current version's front matter and file list |
| `PUT /skills/{skillID}` | `skill.edit` | `{baseVersion, files, comment}` saves the next version; a `baseVersion` that is not current is refused with 409 |
| `DELETE /skills/{skillID}` | `skill.administer` | archives the skill |
| `POST /skills/{skillID}/restore` | `skill.administer` | takes it out of the archive |
| `GET /skills/{skillID}/versions` | `skill.read` | its versions, newest first |
| `GET /skills/{skillID}/versions/{versionNumber}` | `skill.read` | that version's front matter, comment, author and file list: `path`, `bytes`, `sha256` |
| `GET /skills/{skillID}/versions/{versionNumber}/file` | `skill.read` | `?path=` one file's content |
| `GET /skills/{skillID}/diff` | `skill.read` | `?from=&to=` answers `{from, to, files: [{path, status, hunks}]}`: each file added, removed or changed between two versions, a changed one with the workflow diff's hunks |
| `POST /skills/import` | `skill.create`, or `skill.edit` on the skill named | a zip (`application/zip`, at most `MaxSkillImportBytes`) of one skill, `?comment=`; makes the skill, or saves the next version of the one with that name; answers which (#220) |
| `GET /skills/{skillID}/versions/{versionNumber}/export` | `skill.read` | that version as a zip that imports unchanged (#220) |
| `GET /skills/{skillID}/grants` | `skill.read` | who holds which role on it |
| `POST /skills/{skillID}/grants` | `skill.administer`, session | grants a role on it to a person or a group |
| `DELETE /skills/{skillID}/grants/{grantID}` | `skill.administer`, session | revokes one |

A skill the caller may not read answers 404, as a workflow does; one they read
but may not change answers 403.

The route table declares JSON, form and binary bodies: the import row has a
binary request and the export row a binary response, both `application/zip`
in the OpenAPI document (#220).
Pending rows cannot be limited to a session either; the grant handlers refuse
an assistant when #219 builds them, as the workflow grant handlers do.

### Who reads what

- Everyone signed in reads the `skill` rows `actor_can_skill('skill.read')`
  allows and every built-in one, with their versions and files.
- Writing goes through the policies, each tried straight through SQL as
  `rotor_app` in the tests: inserting a skill needs `skill.create` and the
  caller as owner; a version needs `skill.edit` and the caller as author;
  archiving and restoring need `skill.administer`; a built-in skill refuses
  all three to `rotor_app`.
- The worker reads a run's pinned skill files through `rotor_admin`, after
  checking the run's person may read the skill (#222).

### Audit

`skill.created`, `skill.changed` (`{version, comment}`), `skill.shared`
(through the existing `role.granted` and `role.revoked`, naming the skill),
`skill.archived`, `skill.restored`, `skill.imported` (`{version, created}`)
and `skill.installed` (a built-in version the api added, #225).

### Problems a skill is refused with

| Code | Path | When |
|---|---|---|
| `skill_missing` | `-` | no `SKILL.md` at the root |
| `skill_front_matter` | `SKILL.md` | the front matter is missing or not YAML |
| `skill_name` | `SKILL.md` | `name` is missing or breaks the name rule |
| `skill_name_taken` | `SKILL.md` | another skill has the name |
| `skill_name_changed` | `SKILL.md` | a new version names the skill differently |
| `skill_description` | `SKILL.md` | `description` is missing, empty or too long |
| `skill_path` | the path | a path breaks the path rule |
| `skill_binary` | the path | a file is not UTF-8 text |
| `skill_too_large` | the path, or `-` | a file or the version is over its limit |
| `skill_too_many_files` | `-` | more than `MaxSkillFiles` files |
| `skill_not_zip` | `-`, or the entry | an import is not a zip, or an entry in it cannot be read or unpacks to more than its header says (#220) |

### As built (#219)

- **Migrations:** 01400 makes the tables, the numbering trigger and the fixed
  name, owner and `builtin` (`skill_fixed`); 01410 adds the permissions, the
  roles (keys `skill_administrator`, `skill_editor`, `skill_reader`),
  `role_assignment.skill_id`, `actor_can_skill()`, `skill_change_guard` and
  the policies. `actor_can()` now passes over grants on a skill, so a role
  granted on one skill never answers for a workflow or the installation,
  whatever words it holds; Author's and Administrator's lists gain the skill
  words rather than being rewritten, since an administrator may have edited
  them.
- **A version's files are written with it:** `rotor_app` adds a file only in
  the transaction that wrote its version, as that version's author. A skill
  in the archive is refused a save by the service (409 `skill_archived`), not
  by the database, as a workflow is.
- **422 answers carry the problems:** the error envelope gains `problems`,
  each with its `code` and `path`, beside `fields` keyed by path; the code of
  the refusal is `skill_invalid`. A comment over 500 characters is
  `comment_too_long` at `-`, as for workflows.
- **`POST /skills/validate`** checks what the files alone tell; whether
  another skill has the name is answered by the save
  (`skill_name_taken`), and only a save knows the skill whose name must not
  change (`skill_name_changed`).
- **Front matter** keeps every key as written, not only the ones listed;
  `metadata` with a value that is not text is `skill_front_matter`, asking
  for it to be quoted. Line endings may be `\r\n`.
- **The hash** is sha256 over the files sorted by path, each written as its
  path, a NUL byte, its content and a NUL byte; neither holds a NUL, so no
  two sets of files read the same. #225 hashes shipped skills the same way.
- **Responses:** a skill's `owner` and a version's `createdBy` are null for
  a built-in skill; a version also answers its `description` and `bytes`.
  `POST /skills` takes `{files, comment}` alone and `PUT` requires
  `baseVersion`.
- **Grants:** a role granted on a skill must give something there (one of
  `skill.read`, `skill.edit`, `skill.administer`), or it is refused with 422
  `wrong_scope`; a role over the whole installation is refused as on a
  workflow. A grant on a built-in skill is refused by `role_assignment_guard`
  to everyone. `role.granted` and `role.revoked` name the skill as `skill`.
  Grants on skills show on `GET /access/me` (`skillId`, and
  `permissions.skills`), on Admin's grants and Users pages (`skillId`,
  `skillName`), and holding `skill.administer` anywhere lets a person look
  up people and groups to grant to.
- **The line diff** moved from `internal/workflows` to `internal/textdiff`,
  which workflows and skills share.

### As built (#220)

- **Import** reads the body through a `MaxBytesReader` at `MaxSkillImportBytes`;
  a larger one is refused with 413 `too_large`. `SKILL.md` may sit at the zip's
  root or, when nothing sits beside it, in the one folder that holds every
  file, which is stripped. Directory entries are ignored. Before anything is
  unpacked the zip is refused when it has more file entries than
  `MaxSkillFiles` (`skill_too_many_files` at `-`), when the sizes its headers
  declare add up past `MaxSkillVersionBytes` (`skill_too_large` at `-`) or one
  is past `MaxSkillFileBytes` (`skill_too_large` at the entry), or when an
  entry is a symlink or another special file, or has a name that is absolute,
  has a `..` segment or a backslash (`skill_path` at the entry). Entries are
  then read through a limit as well; Go's reader already stops an entry that
  inflates past its declared size, which is `skill_not_zip` at the entry, so a
  lying header cannot get past either. What is left goes through the library's
  `Check`, so every other problem has the library's code and path. A zip with
  no `SKILL.md` at the root or in a single folder (two folders, files beside a
  folder) is `skill_missing` at `-`.
- **Which happened:** the name in `SKILL.md` is looked up among the skills the
  caller reads. None: the skill is made (needs `skill.create`, else 403) and
  the answer is 201 with `created: true`. One: the next version is saved on
  top of its current one (needs `skill.edit`, else 403; an archived skill is
  409) and the answer is 200 with `created: false`; the answer is the skill
  and its current version, as `GET /skills/{skillID}` gives them, plus
  `created`. A name held by a skill the caller cannot read is 404, the same
  words as any skill they cannot see. `?comment=` is the version's comment.
- **Audit:** `skill.imported` with `{version, created}` is written in the
  transaction of the save, beside `skill.created` or `skill.changed`, which an
  import writes as a save does.
- **Export** answers `application/zip` with `Content-Disposition: attachment;
  filename="<name>-v<n>.zip"`. Files sit under one folder named after the
  skill, `SKILL.md` first and the rest by path, each dated 1980-01-01 UTC and
  deflated, so a version exports to the same bytes every time. The zip imports
  with the version's hash.
- **Zip files with an entry starting with a dot** (such as `.DS_Store`, or a
  `__MACOSX` folder) are refused by the path rule like any other file; clean
  them out before zipping.

## Agent nodes name their skills (#221)

```yaml
nodes:
  - id: analyse
    type: agent
    prompt: Turn the story into requirements.
    skills:
      - { name: requirements-engineering, version: 2 }
```

- `skills` is a list of at most `MaxAgentSkills` items, each `{name, version}`
  with no other key; `name` follows the name rule and `version` is a whole
  number from 1. Only agent nodes take it.
- Sense, in Go and Python alike: a name twice on one node is `skill_twice` at
  `/nodes/<i>/skills/<j>`.
- Saving, and `POST /workflows/validate` for the editor, checks each skill as
  the person saving:
  - `skill_unknown`: no skill of that name they may read. One message for
    both, so a name tells nobody that a skill they may not read exists.
  - `skill_version_unknown`: the skill has no such version.
  - `skill_archived`: the skill is archived; a version saved before keeps
    starting, since its pin still holds.
- Starting a run, by hand or by a trigger, checks that the run's person may
  read every skill the workflow version names; otherwise the start is
  refused with `skill_unreadable` and a sentence naming the skill, as a model
  an administrator withdrew is. Sharing a workflow therefore means sharing its
  skills too; the editor says so beside the sharing panel (#223).
- Until #221 merges, the field is in the schema and refused with
  `not_built` at `/nodes/<i>/skills`. The golden fixtures
  `invalid/not-built-skills.yaml` and `invalid/agent-skill-bad-pin.yaml` come
  with this contract; `invalid/agent-skill-twice.yaml` comes with #221, since
  `not_built` refuses any `skills` before a sense check sees a name twice.

### As built (#221)

- **`not_built` is gone again:** the Go and Python refusals and the problem
  code are deleted, as M13's were with #138. `invalid/not-built-skills.yaml`
  became the valid fixture `valid/agent-with-skills.yaml`; beside
  `invalid/agent-skill-twice.yaml`, `invalid/code-with-skills.yaml` holds
  that a code node naming skills is a `schema` problem at `/nodes/<i>`.
- **`skill_twice`** is placed at the second mention of the name, and at each
  mention after it.
- **The save check** asks the library for every named skill in one query as
  the person saving, so row level security answers which they read; a
  built-in skill's versions are the numbers it shipped, which may skip, so
  `skill_version_unknown` asks for the version itself, never a range. The
  check runs only once the spec has no other problem, as the model and image
  checks do, and a library that cannot answer leaves the skills to the start.
- **`skill_unreadable`** answers 422 to a start by hand, and to turning on a
  trigger whose owner may not read a skill. A firing that meets it turns its
  trigger off with the new reason `skill_unreadable`, whose message names the
  agent and the skill. An archived skill the person reads still starts.

## Loading a skill during a run (#222)

### What the run is given

`StartRun` gains `map<string, AgentSkills> agent_skills = 13`, keyed by node
id like `agent_models`:

```proto
message AgentSkills { repeated SkillCard skills = 1; }
message SkillCard {
  string name = 1;
  int32 version = 2;
  string description = 3;
  repeated SkillFileInfo files = 4;
}
message SkillFileInfo { string path = 1; int64 bytes = 2; }
```

It carries no content: a skill's files are read only when an agent asks.

### Reading a file

A request and answer pair on the stream, as checkpoints and tokens are, with
the next free numbers in the `ConnectRequest` and `ConnectResponse` oneofs:

```proto
message SkillFileRequest {
  string request_id = 1;
  string name = 2;
  int32 version = 3;
  string path = 4;
  int64 offset = 5;
}
message SkillFileResponse {
  string request_id = 1;
  string content = 2;
  int64 offset = 3;
  int64 total = 4;
  string refusal = 5;
}
```

The control plane answers at most `MaxSkillReadBytes` from `offset`, cut on a
character boundary, only when the run's pinned spec names that skill version
on one of its agent nodes and the run's person may still read the skill.
Otherwise `refusal` says why, and the agent sees that sentence.

### What the agent sees

- After the role, its instructions list the skills it was given, each as
  `name (version): description`, neutralised as the role is, and say that
  `read_skill` loads a skill's `SKILL.md` and then the files it mentions.
- A built-in tool, offered only to an agent with skills:

  `read_skill(skill: string, path: string = "SKILL.md", offset: int = 0)`

  It answers the text, and when the file goes on, a last line saying which
  `offset` reads the rest. A skill the node was not given, or a path not in
  its file list, answers a sentence saying so. A binding tool of the same
  name refuses the run with `tool_names`, as a workspace tool's clash does.
- Its answer is not fenced as untrusted data: a skill is the workflow's own
  instruction, pinned to the version its author chose. The policy text says
  so, and that a skill never overrides the policy itself. What a tool, a
  person or a page writes stays untrusted, as before.
- Each call is a tool call, so it counts against the agent's `maxSteps`.

### The event

`skill.read`, payload `{node, skill, version, path, offset, bytes}`; never
the content. The run's timeline shows it as the agent reading that skill
file. Added to the event table in `docs/api-contract-m2.md`.

### Testing it

The stub model in `internal/toolstub` records each request's messages and
tool names, so an integration test sees the skills listed and `read_skill`
offered; the runner's `RecordingModel` records bound tools for the same in
unit tests.

### As built (#222)

- **The pair carries no `seq`,** as the checkpoint requests do not: the
  runner asks again after a reconnect, and the answer is never stored to be
  resent. The answer's `offset` is where its `content` starts, in bytes; an
  offset inside a character moves on to the next one, and one below 0 or past
  `total` is refused with a sentence. Reading at `total` answers no content.
- **Who may read:** the control plane asks as `rotor_app` acting for the
  run's person whether they read the skill, so the policies, built-in skills
  included, answer it on every read; the version's card and files are then
  read as `rotor_admin`. A database failure answers a refusal asking to try
  again rather than ending the stream.
- **A resume after a grant was revoked** still starts, with that skill left
  out of `agent_skills`: its description is not read for a person who may no
  longer read it, so the agent's list and `read_skill` leave it out, and the
  control plane would refuse its reads in any case.
- **The cards** list `SKILL.md` first, then the files by path, as the
  library's version does. A node whose skills are all unreadable gets no
  entry, and so no tool and no list.
- **What the agent sees:** the list follows the role as `- name (version):
  description`, each line neutralised. The text `read_skill` answers is
  neutralised too, so a skill cannot open or close a fence, though it is
  never fenced itself. Refusals the runner makes itself, for a skill or a
  path not on the card, name the skills or files there are. The fencing
  middleware takes the names of the tools it leaves unfenced, which is
  `read_skill` alone; the clash check is what makes that name the runner's.
- **The stub model** keeps the last `ToolStubMaxLLMRequests` (50) requests as
  `llmRequests` in a tenant's stats: each message's role and text, its
  content parts joined, and the names of the tools offered.

## The web app (#223)

- **Skills** in the sidebar, for everyone signed in: the skills the person
  reads, built-in ones marked, with archived ones behind a toggle.
- A skill's page: its `SKILL.md` and files, editable with `skill.edit` and
  saved as a version with a comment; Versions, with the diff; Sharing, as a
  workflow's; archiving and restoring with `skill.administer`; export.
- New skill, from a template that passes the checks, and import from a zip.
- In the workflow editor, a skill picker beside the tool picker lists the
  skills the person reads and their versions and inserts
  `- { name: ..., version: ... }` under `skills:` of the agent node at the
  cursor; the canvas inspector shows an agent's skills.
- Strings in `web/src/strings_skills.ts`.

### As built (#223)

- **No API changed.** The pages use the endpoints above as #219, #220 and
  #224 built them; the client's `ApiError` gained `problems`, read from the
  error envelope, so a 422 from a save or an import is shown at its files.
- **Pages:** `/skills` (second in the sidebar, after Workflows), `/skills/new`,
  `/skills/import`, and `/skills/$skillID` with the sections Files (where it
  opens), Versions, Sharing and General in a side nav, as a workflow's
  settings have. Import is a page of its own beside New skill rather than a
  dialog, so the empty state and the list link to both. Archiving and
  restoring, with the Undo, are on General beside the skill's front matter.
- **The list** asks `GET /skills` 50 at a time with `q` after a pause in
  typing and `archived=true` behind the toggle, and pages with the Users
  page's pager. A built-in skill shows a Built in tag and Rotor as its owner.
- **Files** reads every file of the version shown through `/file?path=`
  (versions never change, so each is read once) and shows `SKILL.md` first.
  An older version opens read-only at `/skills/$skillID/files?version=<n>`.
  Editing is a text area per file, the version's files loaded in full; a file
  is added by its path and removed, `SKILL.md` excepted; the files are checked
  with `POST /skills/validate` after the editor's pause in typing and saved
  with `PUT` and the version they were opened at. A 409 `skill_changed` is
  answered with a sentence and a Reload button; 422 problems are listed by
  file and counted beside each file's name.
- **New skill** writes `SKILL.md` alone, from a template that passes the
  checks; other files are added on Files once the skill is made.
- **Export** is a link to `GET /skills/{id}/versions/{n}/export`, which the
  browser downloads with the session's cookie under the name the
  `Content-Disposition` gives; Files offers it for the version shown and
  Versions for each version. Import sends the file as `application/zip` and
  refuses one over 2 MiB before sending it.
- **Sharing** is the workflow's panel made general (`GrantsPanel`), which the
  workflow's Sharing page now uses too: anybody who reads the skill reads its
  grants, a Skill administrator changes them, offering Skill reader, Skill
  editor and Skill administrator, and the page says that Skill reader for
  Everyone makes the skill available to everyone. A built-in skill has no
  Sharing section; its address says why.
- **The picker** asks for up to 200 active skills (the list's most) and a
  skill's versions once picked, newest chosen. It edits the text of the YAML
  document as the canvas's edits do (`canvas/skills.ts`), never a raw insert:
  the agent node is the one whose lines hold the cursor, `skills:` is added at
  the node's key indent after its last value (or as `skills: [...]` in a node
  written as a flow map), and a pin is appended in the list's own style. A
  skill the node names already is pinned to the version picked, so the picker
  never makes `skill_twice`. Outside a node, in a node that is not an agent,
  or in YAML that does not parse, it says so and changes nothing. It reads
  the YAML editor's cursor, also while the Canvas tab is shown. The editor's
  handle gained `cursor()` and `apply(text)`, which changes only the part that
  differs, so the cursor stays where it was.
- **The inspector** lists an agent's skills with their versions, each with a
  Remove button; the last one removed takes `skills:` with it, so adding a
  skill and removing it gives the text back. Adding stays in the picker.
- **The note** about skills sits above the panel on a workflow's Sharing page.
- **Screenshots:** none added; `docs/screenshots.md` asks for no picture per
  page.

## Skills through MCP (#224)

Tool rows on the skill operations, as `docs/api-contract-m10.md` describes:

| Tool | Operation | Kind |
|---|---|---|
| `list_skills` | `GET /skills` | read |
| `get_skill` | `GET /skills/{skillID}` | read |
| `get_skill_version` | `GET /skills/{skillID}/versions/{versionNumber}` | read |
| `get_skill_file` | `GET /skills/{skillID}/versions/{versionNumber}/file` | read |
| `validate_skill` | `POST /skills/validate` | read |
| `create_skill` | `POST /skills` | write |
| `save_skill` | `PUT /skills/{skillID}` | write |

An assistant allowed only to read is offered only the reads. An assistant
never shares, archives, restores or imports a skill: those stay in person.
Zips are not tools; `get_skill_file` reads each file instead.

### As built (#224)

- **The tools are rows** of the operations table with `tool:` and `toolHelp:`;
  `validate_skill` carries `reads: true` as `validate_workflow` does, so a
  grant made to read is offered the five reads and refused `create_skill` and
  `save_skill` in a sentence, as it is `start_run`. `save_skill` is marked
  idempotent as every PUT is; neither write is destructive, as a save adds a
  version and removes none.
- **Nothing new was needed to keep the in-person operations out.** An access
  token is accepted at MCP and for the calls its tools make, nowhere else, so
  the token sent straight to the grant, archive, restore or import routes
  answers 401 `invalid_token` before any handler, and no row of those carries
  a tool, which a unit test holds. The grant routes also take a session alone
  (`session_only`). No code changed for this; integration tests hold it.
- **The 64 KiB cap** applies to every skill answer, so `get_skill` on a skill
  with large files may be refused with the cap's sentence; `get_skill_version`
  lists paths and sizes and `get_skill_file` reads one file, which the
  tools' help says.
- **Who may do what** is unchanged: the person's own policies and roles
  answer every call, so a skill the person may not read is not found, and a
  write needs `skill.create` or `skill.edit`.

## The skills Rotor ships (#225)

- `skills/<name>/` at the repository's root is their single source, as
  `examples/` is for workflows; each `SKILL.md` declares its version as
  `metadata.version`. `make skills-builtin` copies them into
  `internal/skills/builtin` for `go:embed`; a test refuses a stale copy.
- When the api starts, it installs each shipped skill as a built-in: it makes
  the skill when it is missing and adds the declared version when that version
  is missing, as `rotor_admin`, auditing `skill.installed`. A version that
  exists with other content stops the start, with a sentence saying the
  shipped skill changed without a new `metadata.version`. Older versions stay,
  so a workflow pinning one keeps working after an upgrade.
- A fresh installation has only each skill's current version, so the versions
  of a built-in skill may have gaps.
- First `requirements-engineering` and `test-engineering`.
  `story-refinement` and `story-to-requirements` give their analysing agent
  `requirements-engineering`; `requirement-to-test-spec` gives its designer
  and reviewer `test-engineering`; each pinned to the version shipped.

## Problems and warnings

| Code | Path | Issue |
|---|---|---|
| `not_built` | `/nodes/<i>/skills` | #218, gone with #221 |
| `skill_twice` | `/nodes/<i>/skills/<j>` | #221 |
| `skill_unknown` | `/nodes/<i>/skills/<j>` | #221 |
| `skill_version_unknown` | `/nodes/<i>/skills/<j>/version` | #221 |
| `skill_archived` | `/nodes/<i>/skills/<j>` | #221 |
| `skill_unreadable` | a refused start, not a spec problem | #221 |

## What is left for later

- Binary files in a skill, such as images, and running a skill's scripts in a
  workspace.
- Enforcing `allowed-tools`.
- Skills on code nodes, or mounted into a workspace for a coding agent.
- A pin that follows a skill's latest version.
- Keeping a skill in step with a folder in a repository.
- Which runs read which skill, as a page of the skill.
