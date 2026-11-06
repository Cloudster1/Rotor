# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the versioning [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- A workflow's administrators choose how its runs are traced, under its
  settings, Tracing (#259): keep text as the installation does, or none at
  all, in Rotor or at any destination, whatever the installation keeps;
  every enabled trace destination or only the ones chosen; and the share of
  its runs sent to them, from 0 to 1. The page shows whether the installation
  keeps text and which destinations take it, so inheriting says what it
  means. An administrator of Rotor may change it on any workflow;
  `GET`/`PUT /workflows/{workflowID}/settings/tracing` do the same, and each
  change is audited as `workflow.tracing_changed`.

- A Traces page beside Runs lists the traces your own apps sent Rotor over
  OTLP that belong to no run (#257), newest first, 50 a page: each with its
  root span, service, span count, start and whether a span failed or the
  trace was cut. Administrators see every sender's, with who sent each, and
  their own alone with Only mine. A trace opens in the same tree and details
  as a run's trace, the span it failed in chosen, with the same notices for
  expired, cut and text not kept. With no trace yet, the page shows the two
  variables an app sets to send its spans and links the README's section on
  it. Own traces are not MCP tools yet, as the M18 contract leaves them for
  later.

- Two workflows to start from that show what no other example does (#271),
  and `docs/workflows.md` opens its examples with a table of every example
  and the capabilities it shows. `examples/weekly-digest.yaml` is meant for a
  schedule: with no model, it reads the Armature issues a project filed and
  finished in the last 7 days and comments a summary on an issue kept for it,
  with `retention` and the `maxCost`, `maxConcurrent` and
  `toolCallsPerMinute` limits; `docs/triggers.md` shows its schedule.
  `examples/approved-requirement.yaml` drafts a requirement from a need, lets
  a member of the quality group correct the title, statement and rationale in
  the approval's form, and files what they approved in Exciter through a
  binding with `approval: beforeWrite`. A workflow with no agent no longer
  needs a priced model to carry a `maxCost`. The tool stub answers Armature's
  `list_project_issues`, with the NQL a digest sends, for tests.

- An issue triage workflow to start from, `examples/issue-triage.yaml`
  (#270): on a new Armature issue, or run by hand with an issue key, it picks
  up to three labels from those already in use, adds them and comments
  **Labels from Rotor** with the reason. An issue that has labels, or that no
  label fits, is left alone, and a name not in use is never added, since
  Armature would make a new label of it. The tool stub answers Armature's
  `list_labels` and `bulk_edit_issues` (adding labels) for tests.

- Administrators send runs' traces to the OTLP tools their teams use (#246),
  such as Langfuse, Arize Phoenix or Jaeger, under Admin, Traces: at most eight
  destinations, each a name, the whole OTLP/HTTP traces URL, headers Rotor
  keeps in OpenBao and never shows again, and an optional trace link holding
  `{traceId}` and `{runId}`. Each destination gets every run's trace, the
  runner's spans and those that joined the run, beside the run and never
  holding it up; it gets prompt, tool and answer text only once an
  administrator acknowledges that this text leaves Rotor for it. A Test button
  sends one span without text and says what the destination answered. A run's
  trace links to each destination with a trace link ("Open this trace in").
  The same page sets how many days Rotor keeps traces and whether it keeps
  their text, and the run page's notice that text is not kept points
  administrators there. `GET`, `POST /api/v1/settings/trace-destinations` and
  `GET`, `PUT`, `DELETE /api/v1/settings/trace-destinations/{id}` and
  `POST .../{id}/test` keep them; adding, changing and removing one is audited
  as `trace_destination.added`, `.changed` and `.removed`, never with a
  header's value.

- A run's page shows its trace (#245): each step, model call and tool call as
  a tree of spans, each with its kind, its duration and a bar placed on the
  run's time, and the chosen span's details: kind, timing, status and message,
  who sent it, model and tokens, attributes, events with each exception's
  message and stack, and the whole input and output where Rotor kept text.
  Failed spans are marked in words and with an icon, and when the run failed,
  the span it failed in is opened. The page says when a trace expired, reached
  its limit and how many spans it lost, or kept no text, and how long traces
  are kept. Span text shows as text: nothing in it is markup, markdown or a
  link. The tree works by keyboard (the arrows move, open and close, Enter
  shows a span) and to a screen reader; a large trace opens to its nodes and
  shows wide levels 100 spans at a time.
- A run's trace reads through the API and MCP (#244).
  `GET /api/v1/runs/{runID}/trace` answers its spans as a tree, each with its
  kind, timing, status, model, tokens, attributes, events and the first 200
  characters of its input and output where Rotor kept text, and says whether
  the trace is kept, expired or not begun and whether it was cut;
  `GET /api/v1/runs/{runID}/trace/spans/{spanID}` answers one span whole.
  Whoever reads the run reads its trace, and a run anyone else asks for is
  answered as one that does not exist. A run carries its `traceId`.
  `GET /api/v1/traces`, `/traces/{traceID}` and its spans read the traces a
  sender posted of no run, for the sender and administrators. Assistants get
  `get_run_trace`, the tree shortened for a model (failed spans and their
  ancestors first, at most 300 spans in 32 KiB, values cut to 1000 characters
  and given while 48 KiB lasts), and `get_run_trace_span` for one span whole.
- Any LangChain app or tool service sends its spans to Rotor over OTLP/HTTP
  (#243), at `POST /api/v1/otlp/v1/traces`, as protobuf or JSON, gzip or not.
  The app sets two variables and its stock exporter does the rest:

  ```sh
  OTEL_EXPORTER_OTLP_ENDPOINT=https://rotor.example/api/v1/otlp
  OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer%20<token>
  ```

  The token is an access token from Rotor's authorization server whose grant
  may act, or an access token of the realm of any account Rotor lets in, such
  as a service account's from client credentials; a session cookie, a grant
  to read and an account nobody let in are refused. A span whose trace id is
  a run's the sender may read joins that run's trace; any other, a run the
  sender may not read among them, forms a trace of the sender's own, so the
  answer never tells whether a run exists. Tools a run calls get the run's
  `traceparent` with each MCP request, so their spans join the run. A sender
  may send 300 requests and 20000 spans a minute to each api replica, 4 MiB a
  request (16 MiB unpacked) and 2048 spans in it; spans past a limit or a
  check are named in OTLP's partial success, and text is kept only while an
  administrator keeps traces' text.
- Rotor keeps each run's trace (#242). Every run is traced now, and the
  spans its runner sends are stored in Postgres beside being sent on, for
  whoever may read the run's events and nobody else; storing them never holds
  up a run. A trace stops at 10000 spans or 32 MiB and records what it cut.
  An administrator sets how long traces are kept, 14 days by default and at
  most 90, and whether Rotor keeps their prompts, answers and tool calls,
  off by default and sealed with the run's own key when on, through
  `GET` and `PUT /settings/traces`; the worker deletes spans past it. The
  operator's collector still gets the share of runs `ROTOR_TRACE_SAMPLE_RATIO`
  says.
- A run's trace shows every chain, graph node, tool and model call of its
  agents (#241), traced with OpenInference beside Rotor's run, node and MCP
  spans, so a trace in Jaeger or LangSmith shows why a run did what it did.
  Model calls keep their model and token counts. Inputs, outputs and messages
  are recorded only while LangSmith is on, redacted and cut short, and the
  operator's collector never gets them; an exception shows with or without
  them. The model call span is named after the model's class, such as
  `ChatOpenAI`, instead of `llm <model>`, and its prompt and answer are under
  `llm.input_messages` and `llm.output_messages` instead of `gen_ai.prompt`
  and `gen_ai.completion`.
- How runs are traced is agreed (#240): `docs/api-contract-m18.md` says how
  the runner traces every chain, graph node, tool and model call with
  OpenInference, how Rotor keeps each run's trace for whoever reads the run,
  how a LangChain app sends spans to Rotor over OTLP, how a trace is read
  through the API, MCP and the run's page, and how administrators send traces
  to OTLP destinations, the LangSmith setting carried over as one. The trace,
  OTLP and destination routes answer 501 until their issues land.
- Rotor ships the skills `requirements-engineering` and `test-engineering`
  (#225), installed by the api at start as built-ins everyone reads and nobody
  changes. The default workflows use them: the analysing agent of
  `story-refinement` and `story-to-requirements` loads
  `requirements-engineering`, and the test designer and reviewer of
  `requirement-to-test-spec` load `test-engineering`, each pinned to version 1.
  An upgrade that ships a new version of a skill adds it beside the old ones,
  so a workflow pinned to an older version keeps reading its text.
- Skills in the web app (#223). **Skills** in the sidebar lists the skills you
  may read, built-in ones marked, with a search, the archived ones behind a
  toggle and an empty state that says what a skill is. **New skill** starts
  from a `SKILL.md` that passes the checks and checks it as you type; **Import
  a zip** says whether it made a skill or saved the next version of one. A
  skill's page shows its files, `SKILL.md` first, and with Skill editor
  changes, adds and removes them and saves them as a version with a comment,
  each problem shown at its file; Versions opens, downloads and compares
  versions file by file; Sharing grants Skill reader, Skill editor and Skill
  administrator, to Everyone too; General archives and restores, with an
  Undo. In the workflow editor a **Skills** picker beside the tool picker pins
  a skill, at the version you pick, under `skills:` of the agent node at the
  cursor, and the canvas inspector lists an agent's skills and removes one. A
  workflow's Sharing page says that whoever runs it must read its skills too.
- Skills through MCP (#224). An assistant lists, reads and checks the skills
  its person may read and, unless connected to read only, creates skills and
  saves their next versions, with `list_skills`, `get_skill`,
  `get_skill_version`, `get_skill_file`, `validate_skill`, `create_skill` and
  `save_skill`. Sharing, archiving, restoring and importing remain in person.
- Skills import and export as Agent Skills zips (#220). `POST /skills/import`
  takes a zip of one skill folder, `SKILL.md` at its root or in its one top
  folder, and makes the skill or, when you may edit the skill of that name,
  saves its next version; `GET /skills/{id}/versions/{n}/export` answers a
  version as a zip that imports unchanged. A zip that is not one, is over
  2 MiB, holds a path out of the folder, a link, a binary file or more than the
  library allows once unpacked is refused with a sentence naming the file, and
  the import is audited as `skill.imported`.
- Agents load their skills during a run (#222). An agent's instructions list
  each skill it was given, by name, version and description, and the new
  built-in tool `read_skill` reads a skill's `SKILL.md` or another of its
  files when the agent decides to, 64 KiB at a time. What it reads is followed
  as the workflow's own instruction rather than fenced as data, and is
  answered only for a version the run's workflow pins and while the run's
  person may still read the skill. Each read shows in the run's timeline and
  is recorded as a `skill.read` event, without the text.
- An agent node names the skills it may follow, each pinned to a version
  (#221), and `not_built` no longer refuses them. Saving a workflow, and the
  editor's check as you type, refuses a skill you may not read as if there
  were none (`skill_unknown`), a version the skill does not have
  (`skill_version_unknown`), an archived skill (`skill_archived`) and a skill
  named twice on one node (`skill_twice`). A run, by hand or by a trigger,
  starts only when its person may read every skill the version names;
  otherwise it is refused with `skill_unreadable`, naming the skill, and a
  trigger is turned off for that reason. A version saved before its skill
  was archived keeps starting.
- Rotor keeps a library of skills (#219), in the Agent Skills format: a
  `SKILL.md` with a name and a description, and the reference files it points
  to. Each save is a new numbered version that never changes, a save made on
  a version someone else has since replaced is refused, and any two versions
  compare file by file. Skills are shared like workflows, through the new
  Skill reader, Skill editor and Skill administrator roles; whoever creates a
  skill administers it, Author now also creates skills and Administrator holds
  every skill permission. Skills are archived and restored, never deleted.
  The `/skills` routes are built but for import and export, which follow
  (#220); `docs/skills.md` says how to write and share one.
- How a workflow gets skills is agreed (#218): `docs/api-contract-m17.md`
  says how Rotor keeps skills in the Agent Skills format as a versioned,
  shared library, how an agent node pins the ones it may use, how an agent
  loads them during a run, and how they reach the web app and MCP. The schema
  carries an agent's `skills`, and a workflow using it is refused as
  `not_built` until its issue lands; the `/skills` routes answer 501 until then.
- New workflow offers a Start from list: a blank workflow, then every workflow
  Rotor ships by name and description, each with a link to its section of the
  workflows guide (#214). Choosing one fills the editor (undo takes it back)
  and nothing is saved until you save; `/workflows/new?start=<name>` opens it
  directly. `examples/` is the single source of the list.
- A test design workflow to start from, `examples/requirement-to-test-spec.yaml`
  (#213): for an Exciter feature, it designs the test cases its changed
  requirements need, has them reviewed for up to three drafts before a person
  decides, files them as test cases linked to what they verify, clears the
  suspect links it settled, comments the test specification on the feature
  and asks for the cases' review.
- A story refinement workflow to start from, `examples/story-refinement.yaml`
  (#212): it asks on a new Armature story what is unclear, and once answered
  comments a refined story with acceptance criteria as given, when and then,
  what is out of scope, assumptions to confirm and, for a story too large, a
  split proposal. The story to requirements workflow now takes its comments
  for Rotor's, so neither answers the other's questions.
- Settings gathers your own pages (#206), each opening with a sentence on what
  it is for: Profile, Theme, Assistants, Usage and Tool accounts.
  - Profile, which was Account, shows your name, email and groups as the
    suite's sign-in gives them, and this installation's build, and lets you
    choose the time zone every date in Rotor is written in; until you choose
    one, Rotor follows your browser's. The API is `GET` and
    `PUT /auth/me/preferences`, and only you change yours, in person.
    `/settings/account` sends you to Profile.
  - Theme chooses the colour scheme, as the system, light or dark, the same
    choice the top bar's button steps through, kept in this browser.
  - Tool accounts is where you connect your accounts in Exciter, Armature and
    Stator, moved from Connections, which leaves the sidebar. A tool's consent
    now returns to `/settings/accounts`, and `/connections` sends you there,
    with what the tool said.
- Each workflow has settings pages of its own (#205), at
  `/workflows/<id>/settings/<section>`: General (its name and description, and
  archiving it), Versions, Sharing and Triggers, with a side nav as Settings
  has. Everybody the workflow is shared with opens them, and sees read only
  what their grants do not let them change. Saving a new name or description
  saves a new version. The editor keeps the YAML, the canvas and the checks,
  with the Run button and a link to the settings.
  - Archiving offers Undo, which takes the workflow out of the archive again,
    and an archived workflow's General page restores it. Both take
    administering the workflow; the API is `POST /workflows/{id}/restore`,
    audited as `workflow.restored`. Workflows are still archived and never
    deleted, since runs pin their versions; archiving from the list asks first
    in a dialog, and so does removing a trigger.
  - The list says how many people and groups a workflow is shared with and
    what your grants let you do with it, in place of the old sharing badge.
- Signing in is no longer being let in (#204). Somebody the identity provider
  knows who has not been let in waits, and the sign-in page tells them so; their
  request shows above the table on Admin's Users page, where an administrator
  lets them in as a member or turns them away, and somebody turned away may
  ask again by signing in. Everybody who had an account before is let in
  already, so nobody's access changes.
  - "Let people in by their groups", off at first, lets somebody in at sign-in
    when a realm group of theirs holds a grant, with that group's roles. A
    group holding Administrator always lets its members in, so the realm's
    administrators and service accounts keep working.
  - The API: `GET /users/requests`, `POST /users/requests/{userID}/admit` and
    `DELETE /users/requests/{userID}`, for administrators.
- Single sign-on is a tab of Admin's Access page (#204): the issuer, the client
  ID, the client secret, the groups claim and "Let people in by their groups".
  A change applies at the next sign-in, without a restart. The client secret is
  kept in OpenBao and never shown; the page says only that one is stored, and a
  blank one keeps it. A provider that could not work is refused, and so is
  turning it off while it is the only way into Rotor, saying why. The API is
  `GET` and `PUT /oidc-provider`.
- `ROTOR_BOOTSTRAP_MEMBERS` and the chart's `bootstrap.members` let people in by
  their address, as Stator's and Exciter's bootstrap members do (#171): at
  every start of the api, or at their first sign-in when they have not signed
  in yet. It takes Dynamo's `DYNAMO_MEMBERS` as it is (`email=admin`,
  `email=member`, comma separated) and plain addresses, and a later start adds
  names without letting anybody out. A malformed entry stops the start with a
  sentence naming it.
- `ROTOR_OIDC_GROUPS_CLAIM` and the chart's `oidc.groupsClaim` name the claim
  that lists somebody's groups, `groups` unless set.

- Admin's People and access section keeps who may do what, as in Armature
  (#203):
  - Access has the tabs Roles, Matrix and Groups. Roles lists the grants,
    who holds which role and where, with a search, a role filter, granting to
    a person or a group over the installation or one workflow, and taking a
    grant away; the last Administrator grant stays.
  - Matrix shows the permissions down and the roles across, and a tick saves
    at once. Roles of your own are added, renamed, described and deleted, with
    the grants they take with them named first; the built-in roles stay.
  - Groups lists Everyone, then the realm's groups, then those made by hand,
    whose members are added and taken out here.
  - Users lists every person with their groups and roles, and switches a
    person off, which ends their sessions and connections, or on again. Your
    own account and the last active administrator stay on.
  - The API behind them: `/roles`, `/permissions`, `/groups`,
    `/role-assignments`, `/people`, `/users` and a workflow's `/grants`
    (`docs/api-contract-access.md`).

- Access is held as roles, groups and grants, as in Armature (#202):
  - The permissions are `workflow.read`, `workflow.run`, `workflow.edit`,
    `workflow.administer`, `workflow.create` and `rotor.administer`, and the
    built-in roles Administrator, Workflow administrator, Editor, Runner and
    Author hold them. A built-in role is edited but never deleted, and the
    Administrator always administers Rotor.
  - Groups come from the realm's `groups` claim, followed exactly at every
    sign-in, are made by hand, or are the built-in Everyone, which holds every
    person who may sign in and never administers.
  - A grant gives a role to a person or a group over the whole installation or
    one workflow; the last Administrator grant cannot be taken away.
  - `GET /api/v1/access/me` answers your grants and what they let you do, and
    every workflow carries `canRun`, `canEdit` and `canAdminister`, which the
    workflow pages now follow.
  - Nobody's access changes: `rotor-administrators` holds Administrator, a
    workflow's owner is its Workflow administrator, a share is a Runner grant
    to its groups or to Everyone, and Everyone holds Author, so anyone signed in
    still creates workflows. Sharing writes Runner grants from now on.

- A workflow is built on the canvas's graph (#198):
  - Drag a way from a node's bottom handle onto another node to connect them.
  - Let go in the open, or press the "+" under a node, to add a node already
    connected after it, under a free id such as `agent-2`.
  - A selected way can be removed with Delete, or a node inserted into it.
  - Connecting a router asks for the case value or makes it the default.
  - A toolbar adds, inserts, removes, undoes, redoes, zooms and fits. With the
    graph focused, Ctrl+Z, Ctrl+Shift+Z and A do the same.
  - Every change is still an edit of the YAML that keeps its comments, and the
    graph keeps laying itself out.
- An installation that runs no sandboxes, such as Dynamo's compose server,
  refuses to start a run (#178): the api answers 409 `runs_off` with a
  sentence that says it runs no workflows and that an administrator installs
  Rotor with sandboxes to run them, a trigger ends refused with the reason
  `runs_off` instead of retrying, `GET /api/v1/meta` reports `runs: false`, and
  the workflow page and the run dialog say so before Start is pressed.
- Rotor shows each tool's hook address at the place tools deliver to (#181):
  `ROTOR_HOOKS_URL`, chart value `tools.hooksURL`, names the base address the
  tools reach the api at, and `hookURL` of `GET /api/v1/tools`, the `url` of
  `GET /api/v1/tools/{toolID}/hook` and the Admin page use it; without it they
  keep using `ROTOR_APP_URL`. A value that is not an absolute http or https
  address stops the start. The compose stack and the suite set it to
  `http://rotor-api:8080`.
- The integration document says what Dynamo needs to run Rotor (#170): who may
  sign in, the images and memory of the stack and the chart, what an umbrella
  chart sets, and that a compose server runs without sandboxes. It follows
  Dynamo's compatibility rules and the shared promise of `mcp@1`.
- Back-channel logout reaches Rotor under the name the suite gives it (#172): the
  api answers to `rotor-api` on the stack's network, the realm's `rotor` client
  sends logouts to `http://rotor-api:8080/...` as Dynamo's realm does, and
  `make realm-check` now compares the whole realm with Dynamo's (#109).
- A service account of the realm can add tools and hand Rotor their webhook
  secrets (#168), so the suite's `dynamo-connect` wires Rotor to Stator,
  Armature and Exciter unattended. `GET` and `POST /api/v1/tools` and `GET`
  and `PUT /api/v1/tools/{toolID}/hook` take an access token of the realm whose
  audience holds `rotor` and whose groups hold `rotor-administrators`; a token
  missing any of that is refused in a sentence that says what, every other
  route refuses it, and what it changes is audited as the service account.
  Each tool now shows its `hookURL`, and the development realm has the client
  `dynamo-connect`.
- Rotor's chart installs as a dependency of a parent chart, as Dynamo's
  umbrella chart will install it (#169). Every name comes from
  `fullnameOverride`, every secret can come from a Secret made beforehand,
  `openbao.configure.afterInstall` configures an OpenBao the parent brings
  once it is up, `openbao.addr` and `sandbox.egress.openbao.namespace` may
  name the release's namespace as `{{ .Release.Namespace }}`, and a refusal
  names each value as the parent sets it, such as `rotor.oidc.issuer`. The
  chart tests render it under a small parent, and the kube layer installs it
  that way beside a second release.
- A feature specified in Exciter can be implemented through reviewed, tested
  code (#140): `examples/implement-feature.yaml`, started by a person, reads
  the feature's requirements and which of them changed, lets a test designer
  and a test reviewer write its test cases and files them in Exciter, linked
  `verifies`, then lets a coder work on the Armature issue's branch until its
  tests pass and code, security, test run and requirements reviewers agree,
  each round's findings going back to the coder. It opens a pull request with
  the reviewers' summary, records each test case's result in a test execution
  and comments on the issue; nothing merges by itself. The tool-stub places a
  requirement under a section, answers `list_links` with its suspect ends,
  refuses a test case's text sent both as a document and as Markdown, as
  Exciter does, and starts a repository with a fixture's files.
- Each agent can be its own specialist (#133): an agent node may name its
  own `model`, among those an administrator allows, a `role` put under
  Rotor's fixed rules, and its own `maxSteps`, at most the workflow's. Each
  model call counts at its own model's prices toward `maxCost`, the canvas
  edits an agent's role and model, and a run's page shows which model each
  agent ran on and what it used.
- Rotor knows Exciter's test plans and test executions (#139). The suite runs
  a current Exciter (`deploy/suite/EXCITER_REF`), whose tools a workflow
  calls to start an execution of a test plan, record each case's result and
  close it; the catalogue knows these write and ask for an approval, and that
  reading a case's results does not. The tool-stub answers them too, as well
  as `list_types`, `get_tree`, `link_requirements` and `list_test_plans`,
  files test cases with their steps, and the control API reads back the links
  and executions a run made, so the coming feature workflow is tested in
  every pull request. `make tool-snapshots` records Exciter's `tools/list`
  from a throwaway installation, as it does Armature's, without the suite.
  Adding this Exciter works: it lists a token exchange for its partner apps,
  which Rotor now leaves alone instead of registering for it, so Exciter no
  longer refuses Rotor.
- Reviewers run at once and the next step waits for all of them (#135):
  several edges out of a node run their targets in parallel, and `join: all`
  runs a node once, after every branch into it has ended. A node several
  edges lead into with no `join` is saved with the warning `join_missing`.
  Approvals that parallel branches reach together are all in the inbox, the
  run page links to each, and the run goes on once each is decided; one
  rejection ends it. `maxSteps` counts the nodes of every branch, and the run
  page draws the branches side by side above their join.
- Loops end where you say (#134): `maxVisits` on a node bounds how often it
  runs, and the visit past it goes to its `onExhausted`, such as an approval
  where a person takes over, or ends the run as `loop_exhausted`. Templates
  and routers read the count as `visits.<id>`, a loop with no `maxVisits` is
  saved with the warning `unbounded_loop`, the run's timeline numbers each
  visit, and the canvas and the run page draw an edge back as a loop.
- A workflow can run the project's tests as a step and report each one
  (#138): `type: test` with a read-only `workspace`, a `command` the test
  tool writes its JUnit report with (`gotestsum --junitfile`, `pytest
  --junitxml`, vitest's or node's junit reporter), the `reports` to read and
  a `timeout`, 15 minutes unless set. Its output counts passed, failed and
  skipped tests, names the failures and each case with the test case keys its
  `[verifies KEY]` tags name, and keeps the end of the log; failing tests are
  a result, while a command that cannot run, runs out of time or writes no
  report fails the step and says why. Reports are read without following a
  link, at most 4 MiB, and one declaring a DTD is refused. The canvas adds
  test nodes, and the run's page says how the tests went.

- A workflow step can work on a checkout of the run's branch (#136):
  `workspace: {repository, branch, image, access}` on an agent or code node.
  The runner fetches the branch at depth 1, an agent reads, searches, edits
  and runs commands in a toolchain image an administrator allows, and with
  `access: write` what it changed is committed and pushed as the person at
  the step's end; a failed step pushes nothing. Commands run as argument
  lists, never shell lines, with a timeout and capped output; no tool follows
  a symbolic link, and what files and commands say is fenced as data. Code
  nodes get the same through `ctx.workspace`. Administrators allow the
  images, name the one package mirror builds reach, and bound a step's time
  and its work tree in the settings (`/settings/workspaces`). Each image runs
  in a toolchain container beside the runner in the run's gVisor pod, started
  by Rotor's new exec agent image, `rotor-agent`; the chart sizes them and
  lets workspace pods reach the mirror.

- A workflow can bind a repository Armature connects to a project (#137):
  `repository: {project, name}` on an Armature binding, fixed or named by
  the run's inputs, checked with `list_repositories` as the person when the
  workflow is saved or the run starts. A run gets the repository's address
  at Armature's git proxy and its own token for it, with `git.write` when the
  binding writes, so no host token reaches Rotor; `create_issue_branch` and
  `open_pull_request` make the issue's branch and its pull request as the
  person. People connected to Armature before connect again to allow pushes.
  Workspaces (#136) clone and push with it.
- How a workflow writes code is agreed (#131): `docs/api-contract-m13.md`
  says how an agent gets its own model, role and step budget, how a loop is
  bounded, how parallel branches join, how a step checks out the run's
  branch of an Armature repository, and how a test step reports. The schema
  carries the fields, and a workflow using one is refused as `not_built`
  until its issue lands; `/settings/workspaces` answers 501 until then.
- The first workflow that holds a conversation on a ticket (#130):
  `examples/story-to-requirements.yaml` asks on a new Armature user story
  what is unclear, starts again when someone answers, then writes or updates
  the requirements in Exciter, links them to the story, asks for their
  review, and says so on the ticket. Its own comments never start it again.
  `docs/triggers.md` says how to set its trigger up against Armature.
- Rotor is tested against the real Armature (#45, #130): `make suite-up`
  starts Armature beside Exciter, signing in through Rotor's Keycloak, with a
  project `CP` and Exciter connected to it. The suite shows Rotor held until
  an Armature administrator allows it, alice's run commenting as alice with a
  token revoked at the run's end while her grant lives on, Armature refusing
  dave's run, and a story filed in Armature turned into reviewed Exciter
  requirements by Armature's own webhook.
- A trigger condition can say what an event must not be (#126): `does not
  equal`, `is none of` and `is missing` beside `equals`, `is one of` and
  `exists`, so a trigger can leave out, for example, internal comments or a
  bot's events. A path the event does not have equals nothing.
- A workflow no longer starts itself on what its own runs wrote (#127). The
  runner reports the comment, issue or requirement revision each write
  answered with, Rotor keeps it with the run, and an event about it within ten
  minutes skips the same workflow's triggers, saying which run wrote it. A
  skipped firing shows its reason on the workflow page.
- A trigger's owner can let named calls write without an approval (#128).
  A binding lists them under `unattended`, the trigger form asks the owner to
  agree to each, and a later version that names more turns the trigger off
  until they agree again. Each such write is in the audit log. An
  administrator forbids them for everyone under Settings, which turns those
  triggers off at once.
- The tool-stub answers as Armature does, so workflows that read a story and
  comment on it are tested in every pull request (#129). An Armature tenant
  lists Armature's own `tools/list`, recorded at `deploy/suite/ARMATURE_REF`
  by `make tool-snapshots TOOL=armature`, and answers `whoami`, `get_issue`,
  `list_comments`, `add_comment`, `list_remote_links` and `add_remote_link`
  from the issues a test gives it. Exciter tenants also search, create,
  revise, link and send requirements for review, and the control API reads
  back what a run wrote.
- Workflows read and write Armature as the person who started them (#45).
  Rotor finds Armature's OAuth metadata where its MCP endpoint points, waits
  until an Armature administrator allows it, which connecting does, and gives
  every run a token of its own exchanged from the person's connection, so
  ending a run never disconnects anybody. Bindings use Armature's scopes:
  `read`, `issue.write`, `issue.transition` and `comment.write`.
- How a triggered workflow talks on a ticket is agreed (#124):
  `docs/api-contract-m12.md` says how a trigger condition leaves events out,
  how a workflow skips what its own runs wrote, and how a trigger's owner
  lets named writes happen without an approval, which an administrator can
  forbid under `/settings/unattended-writes`. The switch answers 501 until it
  is built.
- The README shows what Rotor does, the way Armature's does (#116): how to
  run it, what it does with pictures of it, how it is put together,
  deployed, tested and operated, every environment variable with its
  default, and where everything lives. `make screenshots` takes the pictures
  in `docs/screenshots.md` from a fresh development stack, as a person would
  use it.
- An assistant can connect by the address of its client ID metadata document
  instead of registering (#114). Rotor fetches the document from the public
  internet alone, through a guard that never reaches Rotor's own network
  unless `ROTOR_OAUTH_FETCH_ALLOW` names a host or range, and the consent page
  says how Rotor knows the assistant.
- Workflows can be drawn on a canvas (#46). The editor has a Canvas tab beside
  the YAML: add, remove and rename nodes, draw the ways between them, set a
  router's cases and the node a workflow starts at, and write each node's
  prompt, call, source or message, all with the keyboard too. Every change is
  written into the YAML, keeping your comments and style, so the YAML tab
  shows what you could have written yourself. Nodes the checks found
  something wrong with are marked, and each problem opens the YAML at its
  line.
- Assistants work with Rotor over MCP as the person who connected them (#47).
  `POST /api/v1/mcp` offers workflows and runs, read, checked, started and
  cancelled, and approvals to read, never to decide. An assistant connects
  with Rotor's address alone: it registers, its person allows it on Rotor's
  consent page, to act or only to read, and Settings lists the assistants
  each person connected, to disconnect any. Administrators see everybody's
  and can switch assistants off. The audit log names the assistant beside
  the person, its CSV export in a last column, `via`, and `GET /runs` takes a
  `limit`. `/meta` lists `mcp@1`.
- Rotor joins the suite's capability names (#43). `GET /api/v1/meta` lists
  `workflows@1`, `runs@1` and `hooks@1`, each held by a test to what the build
  serves, and `docs/integration.md` says what each promises. Rotor reads what
  each tool offers: a tool that does not offer `mcp@1` shows on the
  Connections page as needing an update, and one without `webhooks@1` takes
  no webhook secret. Administrators ask Rotor to read an updated tool again
  with Check again under Settings, Tools, which keeps everyone's connection.
  The nightly suite also runs against the siblings' main.
- Rotor can be released (#44). `./release.sh` works out the next version,
  writes it into every file that carries it, turns this section into the
  release's notes and opens the release as a pull request; merging it tags
  the merge commit, and the tag builds the backend, web and runner images
  and the chart, signs each keyless with the release workflow's identity
  and its SBOM as a signed attestation, publishes them to ghcr.io with the
  GitHub release and its digests, and tells Dynamo. The chart's sandbox
  admission trusts that identity when no key is given, so a cluster can
  refuse any runner image a release did not sign. `docs/releasing.md` says
  how to cut, install and check a release.
- An approval's message, preview and values are ciphertext at rest too
  (#91), sealed with their run's data key; the inbox and the approval page
  read as before. Upgrading deletes the runs kept so far once more.
- Text a run reads cannot quietly steer it (#40). Every agent works under a
  fixed policy, and every value its prompt quotes and every answer a tool
  gives it is fenced as data that nothing inside can close. A link an agent
  writes, in its answer or in what it sends a tool, is removed unless it
  leads to one of the run's tools, and the run's timeline says so and links
  only to those tools. The editor warns, without refusing the save, about a
  workflow that writes after reading with no approval in between. A corpus of
  injection attempts runs in CI against a model scripted to obey them.
- What a run read and wrote is ciphertext at rest (#41). Each run has a
  data key of its own, which OpenBao makes and wraps with the
  installation's key; its inputs, output, events, checkpoints and the
  messages for its runner are sealed with it, so a copy of the database
  shows nothing a tool returned. Credentials of known shapes, from GitHub,
  GitLab, Slack, AWS, Google, Stripe and the model providers to JWTs and
  private keys, are cut from every event, run output, span, audit record
  and log line, by the control plane and the runner alike. Upgrading
  deletes the runs kept so far; the audit log keeps their record.
- Finished runs are deleted once kept long enough (#41): 90 days unless an
  administrator sets another number under Settings, or fewer when a
  workflow says so with `retention: 30d`. The worker deletes a run with its
  events, checkpoints, approvals and usage, and records in the audit log how
  many runs of which workflow went; the audit log itself is never deleted.
- Operators can tell what runs in a sandbox and that it is the image that
  was built (#42). `make sbom`, `sign`, `attest` and `verify` list an
  image's contents as an SPDX SBOM and sign the image and its SBOM with
  cosign; the chart names every Rotor image by digest and, with sigstore's
  policy-controller installed, can refuse any sandbox pod whose image is not
  signed (`sandbox.admission`). The kind test signs the images it installs
  and proves an unsigned one is refused. `make audit-deps` runs govulncheck,
  pip-audit and npm audit, nightly and on pull requests that change a
  dependency, and Renovate proposes updates once a release is two weeks old.
- The threat model (#39): `docs/threat-model.md` names what Rotor protects
  and from whom, goes through each component by STRIDE across its trust
  boundaries, works through untrusted content reaching private data, code
  nodes escaping, token replay and spoofed webhooks, and tracks every open
  finding as an issue. `SECURITY.md` says what is in scope for a report.
- Administrators read and export the audit log (#36): a page under
  Settings filters it by action, person, target and dates, walks it window
  by window, and exports what it shows as CSV or JSON, refusing rather than
  cutting an export too large. The log now also records sandboxes started
  and removed, every run's end and each approval asked for, and each export
  of itself; nobody may change or empty it.
- Runaway runs stop before they cost too much or flood a tool (#38). The
  runner reports each model call's tokens, and Rotor totals them per run,
  person and day, at prices administrators set per model. A workflow bounds
  its tokens (now across every sandbox of a run), its cost, how many of its
  runs go at once and its tool calls per minute; administrators bound each
  person's day and concurrent runs. A run over a limit stops and says why,
  one over a concurrency limit waits queued, and a usage page shows what you
  used.
- Each run is one trace (#37): the api and the worker trace requests,
  statements and the run's sandbox, and the runner's nodes, model calls and
  MCP calls join the same trace through the control plane, so a sandbox
  reaches no collector. `make observe` shows them in Jaeger, with Prometheus
  beside it; the gate runs with them and proves one trace per run. Run
  durations and token mint failures are counted, and the api serves its own
  /metrics. An administrator can send runs' traces to LangSmith, after
  confirming that run data leaves the installation; the key stays in
  OpenBao and never reaches a sandbox.
- Armature, Exciter and Stator can deliver their events to Rotor (#35). An
  administrator adds Rotor's webhook address in the tool and pastes the
  secret it shows into Rotor, which keeps it in OpenBao. Rotor checks each
  delivery's timestamped signature, refuses replays older than five minutes,
  reads envelope v1 and Exciter's CloudEvents, ignores an event it already
  received, and starts every event trigger whose topics, projects and
  conditions match, as the trigger's owner with the event as the input
  `event`. A run starts only if the tool shows the owner the event's
  requirement, issue or page, since a payload is read with the rights of
  whoever added the webhook. On a workflow's page, a person adds such a
  trigger for any tool they are connected to; `docs/triggers.md` walks each
  tool through.
- A workflow runs on a schedule as the person who set it (#34). On a
  workflow's page, add a trigger with cron fields and a time zone, see its
  next runs, choose whether a time is skipped while the last run goes on,
  and give the inputs each run starts with. Rotor checks each firing as you
  and turns the trigger off, saying why on the trigger and the Workflows
  page, once your account, a connection it needs or the workflow is gone.
  Until writes are confirmed at the tool, a triggered workflow must ask for
  an approval before every write.
- The triggers API is agreed (#33): `docs/api-contract-m6.md` says how a
  person schedules a workflow or starts it from Armature, Exciter and Stator
  events as themselves, how Rotor checks the tools' signed deliveries, and
  when it turns a trigger off. Its operations answer 501 until they are
  built.
- A person decides before a run goes on (#31). A run that reaches an
  approval waits, keeping no sandbox and no token, and shows what it is
  about to write. Its starter, or the group the step names, approves it,
  changing the fields the step offers, or rejects it, with a comment; the
  run goes on in a fresh sandbox or ends. An approval nobody decides in
  time ends its run. The Approvals inbox lists what waits for you, with a
  badge in the sidebar and a link to each approval. Decisions are in the
  audit log, and no workflow may call a tool that decides a review.
- The approvals API is agreed (#30): `docs/api-contract-m5.md` says who sees
  and decides an approval, what it shows of the writes to come, how a
  decision resumes a run, and how write confirmation will be held at the
  tool. Its operations answer 501 until they are built.
- A sandbox reaches only what it needs (#27). The chart denies sandbox
  egress by default and allows cluster DNS, the api's runner port, OpenBao,
  and the tools and the model provider by hostname through Cilium (or by
  address range without it). Cloud metadata, the rest of the cluster and
  every other host are refused, which the kind test, now running Cilium,
  proves from a pod with the sandbox's label.
- Rotor installs with one Helm chart (#29), adapted from Exciter's: the api,
  the worker and the web, the migrations, the sandbox namespace under the
  restricted Pod Security standard with the worker's narrow rights, a Job
  that configures OpenBao for the release, and Postgres and Valkey for a
  trial. The chart is linted and rendered against golden files on every
  change, and installed into a kind cluster, where a run executes in a
  sandbox pod, nightly and on pull requests that touch it.
  `docs/deployment.md` says how.
- A run waiting for an answer keeps nothing alive (#28): besides its
  sandbox, the tokens it was handed are now revoked at the tools as soon as
  it starts waiting, and a resume starts with fresh ones. The worker serves
  metrics for operators: the sandboxes alive, those it destroyed and why,
  and the runs by status.
- Runs can execute on Kubernetes (#26). Each sandbox is its own pod under
  gVisor, non-root, read-only, without capabilities or a ServiceAccount
  token, in a namespace under the restricted Pod Security standard; the
  worker may manage pods there and nothing else. In a cluster the control
  plane signs in to OpenBao with its own ServiceAccount, and sandboxes dial
  the api by a configurable name. A kind cluster tests it all, nightly and
  on pull requests that touch it.
- The first run is proven end to end against the real Exciter (#25). In the
  suite, alice runs `examples/draft-requirements.yaml` and the requirement
  appears in Exciter as hers, marked as written through Rotor; dave, who may
  only read the project, runs the same workflow and Exciter refuses the
  write, which his run reports. Afterwards both sandboxes are gone and
  Exciter refuses the tokens the runs held. The example ships in
  `examples/`, checked by both the control plane and the runner.
- People follow a run live (#24). Its page draws the workflow's graph and
  lights it up node by node. Beside it, a timeline says what happened: steps
  started and finished, each tool call with its arguments and answer, what
  the model wrote, and links to what the run created in the tools. A dropped
  connection picks up where it left off. The Runs page lists the runs you may
  see, filtered by workflow, state and the runs you started. The stream works
  through any api replica.
- A run's tokens reach only its sandbox and end with it (#23). Each token and
  the provider key arrive wrapped in a single-use OpenBao token that only the
  sandbox opens, within two minutes; the runner keeps them in memory and cuts
  them from everything it reports, and the control plane redacts events
  again. When the run ends, every token it was handed is revoked at its tool;
  ending a connection or switching a person off revokes them at once, and a
  switched-off person's run gets no new token.
- People start a run of a workflow and it runs as them (#22). Run on a
  workflow opens a dialog that asks for its inputs, says which tools the run
  acts in and offers connecting those you have not, coming back to the
  workflow. A new worker starts each run's sandbox, replaces one that dies
  so the run carries on from its last checkpoint, ends runs past their time
  and cleans up after them; it alone holds the Docker socket now. Runs can
  be cancelled by whoever started them, the workflow's owner or an
  administrator, and a retried start never starts a run twice.
- Administrators choose the language models workflows may use (#20). In
  Settings they add Anthropic, OpenAI, Azure OpenAI or any OpenAI-compatible
  server (such as Ollama), list its models, pick the installation's default
  and test the connection; keys go to OpenBao and nowhere else. A run's
  agents get the workflow's model, or the default, and the key reaches the
  run's sandbox over its channel, never in a file or the environment. The
  tool double gains a scripted, OpenAI-compatible model for tests.
- A run outlives its sandbox (#19). The runner keeps no checkpoint of its
  own: each goes to the control plane over the runner's channel, sealed
  with OpenBao's transit key before it reaches Postgres. A run waiting for
  an answer can lose its sandbox and pick up in a fresh one from the last
  checkpoint, without repeating what it already did. Checkpoints never hold
  a token, and one larger than 4 MiB fails the run, saying so.
- The control plane and a run's sandbox talk over one channel (#18). The
  runner in the sandbox dials the api's runner port over TLS, with a
  certificate from OpenBao's PKI, using a bootstrap token that works once
  and expires within minutes; the api answers with the run's pinned workflow
  and its tools, mints tokens as the run asks for them, and keeps every event
  the runner reports. A dropped link or a restart loses nothing: both sides
  number what they send and resend what the other did not acknowledge, and a
  runner that stops answering is recorded as lost.
- People write, version and share workflows in the browser (#15). The
  Workflows page lists your workflows and those shared with you; the editor
  completes and explains the YAML from the workflow schema and marks each
  problem the server finds on its line as you type, including what only your
  tools can tell, such as a binding that allows a writing tool but says it
  only reads. Every save is a new version that never changes; a save based on
  an older version is refused rather than overwriting a colleague. Versions
  are compared line by line, YAML is imported and exported, and a tool picker
  lists what your connection may call in each tool with its read-only and
  destructive hints. Sharing with your realm groups or everyone lets others
  read and run a workflow as themselves; it never shares your access.
- Runs get sandboxes (#21): in development the api starts each run's runner
  as a container on the host's Docker daemon, read-only with a small `/tmp`,
  without capabilities, as a non-root user, within memory, CPU and process
  limits, on an internal network that reaches the api and the tools and not
  the internet. At start the api destroys sandboxes left from before.
  `make up` builds the runner image.
- Workflows can run a small Python step (#17): a code node's
  `run(state, ctx)` runs in its own process with none of the run's tokens or
  keys in its environment, its own memory and time limits (256 MiB and 30
  seconds unless it says otherwise, at most 1 GiB and 5 minutes) and no file
  writes. It calls tools only through `ctx.call_tool`, which the runner
  checks against the node's bindings; it must return JSON matching its
  output schema, and what it prints becomes capped log events.
- People write, version and share workflows in the browser (#15). The
  Workflows page lists your workflows and those shared with you; the editor
  completes and explains the YAML from the workflow schema and marks each
  problem the server finds on its line as you type, including what only your
  tools can tell, such as a binding that allows a writing tool but says it
  only reads. Every save is a new version that never changes; a save based on
  an older version is refused rather than overwriting a colleague. Versions
  are compared line by line, YAML is imported and exported, and a tool picker
  lists what your connection may call in each tool with its read-only and
  destructive hints. Sharing with your realm groups or everyone lets others
  read and run a workflow as themselves; it never shares your access.
- The runner turns a workflow into a LangGraph graph (#16): agents built on
  the workflow's model with the tools their bindings allow (only tools that
  say they only read, unless a binding may write), single MCP calls with
  arguments filled from the run's state, routers, and approvals that wait.
  Every MCP call carries the run's token for its binding, which never enters
  the run's state. A run that passes its step, time or token limit ends and
  says which; the runner reports each node starting and finishing, the
  model's text and each tool call as events.

### Changed

- LangSmith is a trace destination like any other (#246): an administrator
  adds it under Admin, Traces from the LangSmith preset, its key and project
  being the headers `x-api-key` and `Langsmith-Project`. Admin's LangSmith page
  is now Admin, Traces.

- The built-in Author role says it creates workflows and skills of their own,
  as it has granted since #219. An installation whose administrator reworded
  the role keeps their words (#236).
- Sharing is the workflow's grants alone (#205). `PUT /workflows/{id}/sharing`
  is gone, and so are `sharing` and `sharedGroups` on a workflow, which now
  carries `sharedWith`; grant on `/workflows/{id}/grants` instead. The
  `workflows@1` capability promises the grants operations in its place. The
  migration keeps everyone's access: runner grants already held what sharing
  said.
- `app_user.role` is gone (#205): whether somebody administers Rotor is asked of
  their grants alone, and sign-in no longer writes `user.role_changed`.
  `/auth/me` still answers `role`, computed from the grants.
- Single sign-on is kept in Rotor's database (#204). `ROTOR_OIDC_ISSUER`,
  `ROTOR_OIDC_CLIENT_ID` and `ROTOR_OIDC_CLIENT_SECRET` seed it on the first
  start, so a deployment that sets them keeps signing people in; after that
  the database's provider is used, and the api's log says when the two differ.
  The OpenBao policy of the control plane gains `settings/oidc/*`, which the
  chart and the OpenBao image carry.

- A workflow's Sharing is its grants (#203): anybody who sees it reads who
  holds which role there, and whoever administers it grants people or groups
  Runner, Editor or Workflow administrator. `PUT /workflows/{id}/sharing`
  still works and writes Runner grants, but is deprecated.

- An administrator of Rotor now saves versions of and shares every workflow,
  as the Administrator role holds every permission; before, they saw and
  archived everyone's workflows but changed only their own (#202).

- Settings and Admin are laid out as pages with a side nav, each using the
  page's width, instead of one long column of narrow cards (#199). Settings
  holds your own Account, Assistants and Usage; Admin, which the sidebar
  shows to administrators alone, holds Work (Workspaces, Unattended writes),
  Integrations (Model providers, LangSmith, Tools, Connected apps) and
  Records (Audit log, everyone's Usage and the limits, Retention). Anybody
  else who opens an Admin page is told it is for administrators. Members no
  longer see the list of tools in Settings. The audit log moved to
  `/admin/audit`, and the old `/settings/audit` link leads there. Below a
  wide screen the side nav is a row that scrolls sideways.
- The sandbox namespace is named from the release by default,
  `<release namespace>-<fullname>-sandboxes`, or `<fullname>-sandboxes` when
  the two are the same, so two installs in one cluster never share one
  (#169). An install that relied on `<release namespace>-sandboxes` sets
  `sandbox.namespace` to keep it. The `ClusterImagePolicy` objects that
  admit signed sandbox images are named after that namespace too.

### Removed

- The LangSmith setting (#256): `GET` and `PUT /api/v1/settings/langsmith`,
  `DELETE /api/v1/settings/langsmith/key` and `ROTOR_LANGSMITH_ENDPOINT` (the
  chart's `telemetry.langsmithEndpoint`) are gone, as is `/admin/langsmith`.
  LangSmith is a destination under Admin, Traces; use
  `/settings/trace-destinations`. A development database whose LangSmith key
  the api of #246 never moved into its destination is refused by the
  migration, which says to start the previous version once first.

### Fixed

- The dev and CI stack starts OpenBao more reliably and says why when it does
  not (#278). OpenBao's entrypoint now waits until the node is active before
  its first write, where it used to exit 2 now and then right after unsealing,
  and retries the JWT auth configuration that fetches Keycloak's keys. A
  `make stack-up` that fails saves the logs of every service that is not
  healthy to `reports/stack-logs/`, which CI uploads with the other reports.

- `make up` no longer breaks every tool and connection on the dev stack: the
  tool stub keeps its OAuth clients, grants and tokens on a volume, so
  recreating it with a rebuilt backend image leaves Rotor's connections
  working. `make tool-stub-reset` forgets them on purpose (#268).

- A tool's name changes only when an administrator changes it, and no two
  tools share one (#267). Adding a tool under a name another has is refused
  with a sentence asking for another, and Admin, Tools takes an optional name
  when adding, for a second Exciter beside the first. Administrators rename a
  tool under Admin, Tools, which lists the workflows that bind the old name
  first; renaming is audited. Check again no longer renames a tool after the
  product its metadata names: it shows the product on the tool's row and
  keeps the name workflows bind. Tools that already share a name are listed
  on Admin, Tools to be renamed, and a workflow binding such a name says so
  when it is validated or saved and when a run starts, where it used to say
  the tool "is not set up in Rotor".

- A request its client gave up on, such as a page closed while the workflow
  list loads, is no longer logged as `level=ERROR ... 500 context canceled`;
  it is noted at debug level and shows as status 499 in the access log (#269).

- Rotor says and acts on the true state of a tool and of your connection to
  it (#266). The workflow editor's tool picker tells a tool that must be
  registered again (an administrator does it under Admin, Tools) from a
  connection the tool ended (you connect again, with a button there), where
  it used to ask you to connect an account that read Connected. Check again
  under Admin, Tools now proves Rotor's client with a token request, so a
  tool that forgot Rotor reads Needs registering again instead of Ready.
  Registering again marks everyone's connection to the tool to be connected
  again. A run is refused before it starts, and the run dialog says so, when
  a tool it uses must be registered again. A run whose token could not be
  minted says why and what to do, and no longer says that its tool "cannot be
  reached" at an address inside Rotor's network. The warning badge on
  Admin, Tools now has enough contrast in the light theme.

- A failed span's status message on the run page holds the error alone, such
  as `RunEnded: file raised ValueError: The story has no title (line 2)`, and
  no longer ends with a stray `Traceback (most recent call last):`; the whole
  stack trace stays in the span's exception event (#254).

- The page scrolls inside the app and never past it: the sidebar and top bar
  stay put at the end of a long page, and moving focus to the editor's
  Import YAML button no longer scrolls the window and blanks the screen on a
  phone. A visually hidden control at the end of a page used to stretch the
  window below the app (#264).

- The workflow canvas no longer traps the page. A wheel over the canvas, or
  over a run's graph, scrolls the page, and a pinch or Ctrl and the wheel
  zooms the graph; on a touch screen a swipe over it scrolls the page, the
  canvas leaves room on a phone's screen to scroll past it, and the graph
  can no longer be flung out of reach. The toolbar sits above the graph
  instead of over its first nodes, and fitting draws a small workflow no
  larger than life. A node added from the Add a node form is brought into
  view as one added from the toolbar is, and on a narrow screen choosing a
  node scrolls to its inspector below the canvas (#265).

- A run stops at its approval, and the approval opens, as soon as the runner
  reaches it, whatever the trace collector does. The control plane used to
  send a runner's last spans on before it handled the run's end, so with the
  collector down or slow a run stayed running for about a minute while the
  export retried. Runner spans now go on beside the run, each batch for at
  most 10 seconds, and a batch is dropped while 32 are still on their way.
  The approvals end-to-end tests also give each sandboxed run its own time,
  since two of them in turn did not fit the 20 seconds of a test (#234).

- A push that changes only a shipped skill under `skills/` runs the Go layers
  in the push gate, so a skill out of step with the copy embedded in the api
  is caught before CI (#235).

- The web tooling takes minimatch 11.0.4 in place of 10.2.6, through an npm
  override, which closes the globparts amplification advisory
  GHSA-g3hv-75wg-m249. eslint still asks for 10, and no 10.x is patched
  (#194).

- The runner ships opentelemetry-api and opentelemetry-sdk 1.45.1, past
  CVE-2026-114603, and certifi 2026.10.18, past PYSEC-2026-4320, so
  `make audit-deps` passes again (#190, #189).
- Signing in to Rotor accepts return addresses only on localhost and 127.0.0.1
  themselves; `http://localhost*` also let in hosts that merely start with
  them, such as `localhost.example`. The realm matches Dynamo's again, so
  `make realm-check` passes (#166).
- A workflow with a `maxTokens` of a million or more is saved; such a number
  was written with an exponent on its way into the spec, and refused.
- The run page draws every node of its graph when the run's events arrive in
  a burst, as they do on opening a finished run; a node redrawn before it was
  measured stayed hidden, with its edges.
- A trigger on Armature's events matches its projects and starts its run
  (#125). Rotor reads the issue key where Armature sends it, at the top of
  the payload, and checks the person can read that issue before the run
  starts.
- The development realm carries the token-exchange audience mappers Dynamo's
  canonical realm added to Armature, Exciter and Stator, and `make
  realm-check` compares the realm Dynamo holds, leaving Rotor's own clients
  and group aside until Dynamo carries them (#109).
- A workflow that writes through a binding asking for an approval cannot
  start without one after a tool turned a call into a write (#90). Every
  start, by hand or from a trigger, checks the workflow against the tools
  read afresh as the run's person, and refuses with `tools_changed`.
- OpenBao keeps a record of who read Rotor's secrets (#92): the
  development stack declares an audit device, and Rotor's init script
  refuses an OpenBao without one. The chart and the api refuse a Valkey
  without a password outside development, and `valkey.tls` reaches one over
  TLS.
- A workflow's code node can no longer read the runner it runs under
  (#89). The runner makes itself not dumpable before it reads anything
  secret, so a code node cannot trace it or open its memory, environment
  or files, where its tool tokens and the provider key are.
- A person signing in to Rotor for the first time from two browsers at
  once lands signed in in both, instead of the second failing (#96).
- Signing a person out of every session in Keycloak ends all their Rotor
  sessions, not only the one its logout token names (#53). Rotor reads the
  time of that sign-out from Keycloak a moment after the token arrives, with
  the `rotor` client's service account, which now holds `view-users`; ending
  one Keycloak session still ends only the Rotor session it signed in.
- A run resumed after an approval answers only that approval: a sandbox
  replaced later no longer replays the decision to the next one.
- Rotor reads a workflow's YAML as YAML 1.2, as the runner does (#67): a
  scope such as `mcp:read` inside an inline mapping is text, not a nested
  mapping, and a golden example in flow style holds both sides to it. The
  refusals for an unknown tool and a missing connection now say what to do.
- The tool test double answers `whoami` in the shape Exciter's tool list
  declares, which MCP clients check, and a contract test keeps it so.
- Workflows have a written form (#14): versioned YAML, `rotor.dev/v1`, with
  inputs, tool bindings that say what a run may call, a model, limits, and
  agent, tool, code, approval and router nodes. Rotor checks it against
  `api/workflow.v1.schema.json` and its own sense, and names each problem by
  line and column; the runner checks it the same way, and both are held to
  the same examples. `docs/workflows.md` explains it with three examples.
- The plan for workflows and runs is agreed (#13): `docs/api-contract-m2.md`
  settles the workflow and run operations, the run states, the live events,
  the sandbox interface and the runner protocol, which `proto/` defines and
  `make proto` generates for Go and Python; `api/workflow.v1.schema.json` is
  the workflow spec's first schema. The new endpoints answer 501 until they
  are built.
- The control plane mints a run's access token from a person's connection
  (#12). Each run gets its own, never stored or shared, within what the
  person consented to; asking for more is refused, naming what is missing.
  Only one refresh runs per connection at a time and the rotated refresh
  token is in OpenBao before the access token is used, so twenty runs at
  once never end the grant. A grant the tool ended marks the connection
  "connect again", and one run's token can be revoked alone.
- People connect their accounts in the tools (#11). The Connections page
  lists every tool; connecting sends the browser to the tool's own consent
  page and back, with PKCE, a state bound to the session and the tool's MCP
  endpoint named as the resource. The refresh token goes to OpenBao and the
  access token is dropped. The page shows what was allowed, when it was
  connected and last used, and disconnecting revokes the grant at the tool.
  Removing a tool or switching a person off ends their connections too.
- Administrators add the tools Rotor acts in (#9). Settings lists every
  tool with what it offers; an administrator adds one by the address it is
  opened at, and Rotor reads its MCP endpoint and authorization server and
  registers itself there as an OAuth client, keeping the client secret in
  OpenBao. A tool without OAuth is refused with a sentence saying what it
  lacks; one that forgot Rotor is registered again with a click; removing a
  tool forgets Rotor's client there.
- Rotor can be tested against tools without mocking them (#8). `make up`
  starts a test double that behaves as Exciter does as an OAuth authorization
  server and MCP endpoint, as many independent tools as tests need, kept
  honest by contract tests against Exciter's, Armature's and Stator's own API
  documents and a recording of Exciter's MCP tools. `make suite-up` runs the
  real Exciter beside the stack, signing in through the same realm, and
  `make test-suite` tests against it; CI does so nightly and on pull requests
  labelled `suite`.
- Rotor keeps its secrets in OpenBao (#10). The api signs in to OpenBao as
  the realm's service account `rotor-control-plane`, renews its token and
  signs in again when it ends; it may touch people's connections and the
  tools' clients and nothing else, and every write names the version it
  read. `/readyz` reports unavailable while OpenBao is sealed or
  unreachable. The development OpenBao now keeps its data across restarts.
- The plan for tool connections is agreed (#7): `docs/api-contract-m1.md`
  settles how tools are added, how people connect their accounts, where the
  secrets live and how a run gets its token. The new endpoints are in the
  API document and answer 501 until they are built; those for administrators
  alone already refuse everyone else.
- People sign in with their suite account (#6). Rotor sends the browser to
  the realm `dynamo` and keeps the session as an HttpOnly cookie whose
  SHA-256 alone is stored; `rotor-administrators` makes an administrator,
  everyone else in the realm is a member, and the role follows the realm at
  every sign-in. Ending a session in Keycloak ends it in Rotor through
  back-channel logout, and a switched-off person loses their sessions and
  cannot sign in again. The settings show who is signed in.
- Rotor has a face (#5): `make up` now serves the web client, a shell with
  Workflows, Runs, Approvals, Connections and Settings, each saying what will
  live there, in the suite's light and dark colours. Settings names the build
  the server runs. A browser suite checks every page with axe in both schemes.
- The workflow runner has a home (#4): `runner/` is a Python 3.13 project
  with LangGraph, langchain-core, langchain-mcp-adapters and grpcio pinned
  by `uv.lock`, checked by ruff, mypy and pytest, and built into a
  distroless, non-root image that proves itself with no network, no
  capabilities and a read-only root.
- The backend answers (#3): `make up` now migrates the database and starts
  the api, which serves `/healthz`, `/readyz`, `/api/v1/openapi.json` and
  `/api/v1/meta`. One route table drives the router and `api/openapi.json`;
  sign-in's routes are agreed and answer 501 until #6. The audit log is
  append-only and readable by administrators alone, which the database
  itself enforces.
- The architecture and the decisions behind it are written down (#2):
  a Go control plane and a Python LangGraph runner, Rotor as an OAuth
  client of each tool with refresh tokens kept in OpenBao, one sandbox per
  run that is destroyed when idle, and workflows as versioned YAML. See
  `docs/architecture.md` and `docs/decisions.md`.
- A development stack and a test gate (#1). `make up` starts Postgres,
  Valkey, Keycloak with the suite's realm `dynamo` and an OpenBao dev server
  configured for Rotor, on ports of the checkout's own; `./run-tests.sh` runs
  the same layers locally and in CI, and the realm gains Rotor's sign-in
  client, its control-plane service account and the `rotor-administrators`
  group.
