# Skills

A skill is a set of instructions an agent follows when the work calls for it:
how to write requirements, how to design tests, how your team words a story.
Rotor keeps skills in a library, versioned and shared like workflows, so the
agents of many workflows follow the same proven text. The format is
[Agent Skills](https://agentskills.io), the one Claude Code and other agents
read, so a team brings the skills it has. `docs/api-contract-m17.md` has the
details.

Rotor stores, versions and shares skills (#219), an agent node names the
skills it may follow (#221), and the agent loads them during a run (#222).
Importing and exporting a skill as a zip (#220), the skills pages in the web
app (#223) and the skills Rotor ships (#225) follow.

## What a skill is

A skill is the files of one folder. `SKILL.md` sits at its root and begins with
front matter between two lines of `---`:

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

- **name** is the skill's name in Rotor and in every workflow that uses it:
  lowercase letters, digits and single dashes, starting and ending with a
  letter or digit, at most 64 characters. Names are unique in the
  installation, and a skill keeps its name for good.
- **description** says what the skill does and when to use it, in at most
  1024 characters. An agent sees the name and description alone and decides
  from them whether to read the rest, so write it for that decision.
- **license**, **compatibility**, **metadata** and **allowed-tools** are kept
  as written and shown, but Rotor enforces none of them: what an agent may
  call is still its node's bindings. `metadata` maps names to text, so quote a
  number, as `version: "2"`.

The other files are references the instructions point to, such as
`references/templates.md`.

## Limits

- At most 50 files in a version, `SKILL.md` included.
- At most 256 KiB a file and 1 MiB for all of a version's files.
- Paths are relative, at most 5 folders below the root, without `..`, a
  leading `/`, a backslash or a name starting with a dot.
- Every file is UTF-8 text. Images and programs are left out for now.

A save that breaks one of these is refused, naming each problem and the file
it is in. `POST /skills/validate` checks files the same way without saving.

## Versions

Every save makes a new version, numbered 1, 2, 3, with a comment of at most
500 characters. A version never changes: editing a skill makes the next one,
and nobody uses it until a workflow names that version. That is what lets an
agent follow a skill as it follows its role: the workflow's author chose the
exact text.

A save names the version it was made from. When someone saved in the
meantime, it is refused, so nobody overwrites a colleague unseen; reload and
make the change again. Versions are listed newest first, and any two can be
compared file by file: each file added, removed or changed, with the lines that
changed.

A skill is archived, never deleted, so a workflow that names one of its
versions keeps meaning what it meant. Archiving takes it out of the list;
restoring puts it back.

## Importing and exporting

A skill travels as a zip in the Agent Skills format, the same as a skill
folder from Claude Code zipped up.

- **Import** (`POST /skills/import`, the zip as the body, `?comment=` for the
  version's note) takes `SKILL.md` at the zip's root or inside one folder that
  holds everything. If no skill has the name in `SKILL.md`, it is made, and
  you own it; if one has, and you may edit it, the next version is saved on
  top of its current one. The answer says which happened (`created`). A skill
  you cannot read answers as if it were not there (404), and one you read but
  may not edit is refused (403): make a skill under another name instead.
- **Export** (`GET /skills/{id}/versions/{n}/export`) answers that version as
  `<name>-v<n>.zip`, its files in one folder named after the skill. The same
  version always gives the same bytes, and importing the zip gives the same
  files again.

An import is refused, naming the file, when the body is not a zip or is over
2 MiB; when `SKILL.md` is missing, or sits in a folder with other files beside
it or among several folders; when an entry is a link, or its path is absolute
or holds `..` or a backslash; when the files add up to more than the limits
above, judged on their unpacked size before anything is unpacked, so a zip
bomb is refused; and for everything a save refuses: front matter without
`name` or `description`, a binary file, a hidden file such as `.DS_Store`.

## Sharing

Skills are shared as workflows are, through roles granted to a person or a
group:

| Role | May |
|---|---|
| Skill reader | read the skill and its versions, and give it to the agents of their workflows |
| Skill editor | also save new versions |
| Skill administrator | also share, archive and restore it |

Whoever creates a skill is its administrator. Anyone with the Author role,
which Everyone holds unless an administrator changed it, creates skills, and
an administrator of Rotor holds every role on every skill. To make a skill
available to everyone, grant Skill reader to the Everyone group. Sharing is
done in a browser session, never by an assistant.

A skill you may not read answers as if it did not exist. Sharing a workflow
does not share its skills: a run's person must be able to read every skill the
workflow names, as the next section says.

## Using a skill in a workflow

An agent node names the skills it may follow, each pinned to a version:

```yaml
- id: analyse
  type: agent
  prompt: Turn the story into requirements. {{ inputs.story }}
  skills:
    - { name: requirements-engineering, version: 2 }
```

- A node names at most 10 skills, each once. Only agent nodes take skills.
- Saving the workflow, and the editor as you type, checks each skill as you:
  one you may not read is refused as if there were none (`skill_unknown`), a
  version it does not have is `skill_version_unknown`, and an archived skill
  is `skill_archived`.
- A run starts only when its person, whoever starts it by hand or owns the
  trigger that starts it, may read every skill the version names; otherwise
  it is refused with `skill_unreadable`, naming the skill, and a trigger is
  turned off. Share the skills with whoever runs the workflow.
- A pin never moves: a new version of the skill reaches the workflow when you
  save it naming that version. Archiving a skill keeps the versions saved
  before it starting, since their pins hold.

## What an agent sees in a run

An agent does not get its skills' text up front: it gets a list, and reads a
skill when the work calls for it, so a skill costs tokens only when used.

- **Its instructions** list, after its role, each skill it was given as
  `name (version): description`, and say how to read them. The list is
  checked like the role, so nothing in a description can pass for data Rotor
  fenced.
- **The tool `read_skill`** is offered to an agent with skills, and to no
  other: `read_skill(skill, path = "SKILL.md", offset = 0)`. It answers
  `SKILL.md` unless the agent names another file of the skill, such as
  `references/templates.md`. A file over 64 KiB comes in parts: the last line
  of a part names the `offset` that reads on.
- **What it answers is followed, not fenced.** A skill is the workflow's own
  instruction, pinned to the version its author chose, so the agent follows it
  as it follows its role; what tools, people and pages write stays fenced as
  data. A skill never overrides Rotor's policy for the agent, and its
  instructions say so.
- **Each read is a tool call**, so it counts against the agent's `maxSteps`.

`read_skill` answers a sentence rather than the file when the agent names a
skill it was not given or a file the skill does not have, and when the run's
person may no longer read the skill: whether they may is asked on every read,
so revoking a grant takes effect in runs already going. A binding tool named
`read_skill` refuses the run with `tool_names`; rename it with the binding.

A run whose person lost a skill before a resume still resumes, with that
skill left out of the agent's list.

## Reads in a run

Each read shows in the run's timeline as the agent reading that file of that
skill, at its version, and is recorded as a `skill.read` event with the node,
skill, version, path, offset and bytes read, never the text itself. The text
stays in the library, at the version pinned.

## Skills Rotor ships

Rotor will ship skills of its own, such as `requirements-engineering` and
`test-engineering`, which its example workflows use (#225). They are marked
built-in: everyone signed in reads them, nobody changes, archives or shares
them, and an upgrade adds new versions without removing old ones. To change
one, make a skill of your own from its files.
