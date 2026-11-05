---
name: requirements-engineering
description: Requirements engineering method for eliciting, writing, reviewing, decomposing and tracing requirements at stakeholder, system and software level. Use whenever the task is to capture needs, write or rewrite requirements, user stories or acceptance criteria, review a specification for quality, derive child requirements, set requirement attributes, quantify non-functional requirements, or build and check traceability and change impact.
license: Apache-2.0
metadata:
  version: "1"
---

# Requirements engineering

Turn stakeholder intent into a set of requirements that is necessary, unambiguous,
verifiable and traceable, at the right level of abstraction. Grounded in ISO/IEC/IEEE
29148 and the INCOSE guide for writing requirements.

Project conventions win: if the repository or tool already has an ID scheme, template,
attribute set or wording rule, follow it and use this skill only to fill gaps.

## Workflow

1. **Establish context.** System of interest, its boundary, the stakeholders and their
   concerns, the operational scenarios. Read what exists first (specs, issues, tickets,
   code, tests) instead of inventing needs. If no stakeholder is known for a need, say so.
2. **Elicit needs.** Capture each need in the stakeholder's words with its source. Ask
   about goals, scenarios, exceptions, constraints (legal, regulatory, technical,
   budget), quality expectations and what must never happen.
3. **Transform needs into requirements** at the right level (see Levels). One
   requirement states one thing. Record rationale for every requirement.
4. **Analyse the set.** Check each requirement against the characteristics below, then
   the whole set for completeness, consistency, conflicts and feasibility. Resolve
   conflicts with the stakeholders, not silently.
5. **Assign attributes**, at least: ID, text, rationale, type, source, priority, status,
   verification method, parent trace.
6. **Validate** with stakeholders ("is this what you need?") and confirm each
   requirement can be verified ("how will we prove it?").
7. **Baseline and manage change.** Changes go through impact analysis along the trace
   links; links to changed items become suspect until reviewed.

Mark every assumption explicitly as an assumption and every open point as TBD with an
owner. Never fill a gap with a plausible guess presented as fact.

## Levels

| Level | Says | Example subject |
|---|---|---|
| Stakeholder need | what a person wants to achieve, in their words | "The operator needs to..." |
| Stakeholder requirement | what the solution must provide to them, solution-neutral | "The system shall allow the operator to..." |
| System requirement | what the whole system must do or be, black box | "The system shall..." |
| Subsystem / software requirement | what an allocated element must do | "The billing service shall..." |
| Design | how; not a requirement | belongs in architecture or design |

Keep "how" out of a requirement unless it is a real constraint imposed from outside
(then type it as a constraint and name who imposed it). A child requirement must be
justified by a parent; a requirement with no parent is either a stakeholder
requirement, a derived requirement with recorded rationale, or unnecessary.

## Characteristics of a good requirement

- **Necessary**: removing it would leave a need unmet.
- **Appropriate**: at the right level; no design at system level.
- **Unambiguous**: one reading only; terms defined in a glossary.
- **Complete**: needs no other text to be understood; conditions and units stated.
- **Singular**: one capability or constraint; no "and", "or", "/" joining two.
- **Feasible**: achievable within known constraints.
- **Verifiable**: a test, analysis, inspection or demonstration can decide pass or fail.
- **Correct** and **conforming** to the project's template and vocabulary.

For the set: complete (all needs covered), consistent (no conflicts, one term per
concept), feasible as a whole, comprehensible, and able to be validated.

## Writing rules

- Use "shall" for binding requirements, "should" for goals, "may" for permission,
  "will" for statements of fact about the environment. Never mix within one statement.
- Active voice, the responsible subject first: "The system shall...", not "It shall be
  possible to...".
- State conditions and triggers explicitly; prefer the EARS patterns:
  - Ubiquitous: `The <system> shall <response>.`
  - Event-driven: `When <trigger>, the <system> shall <response>.`
  - State-driven: `While <state>, the <system> shall <response>.`
  - Unwanted behaviour: `If <condition>, then the <system> shall <response>.`
  - Optional feature: `Where <feature is included>, the <system> shall <response>.`
  - Complex: combine `While ... when ...` in that order.
- Quantify with units, tolerances and the measurement condition.
- Avoid vague terms, and replace them with a measurable criterion: user-friendly, easy,
  fast, efficient, robust, flexible, scalable, sufficient, adequate, appropriate,
  as possible, minimal, maximal, state of the art, etc., and/or, including but not
  limited to, support (without saying what), normally, typically, usually.
- No negatives that cannot be tested ("shall never fail"); state the required
  behaviour or the acceptable failure rate instead.
- No pronouns referring to other requirements; no references to "the above".

## Non-functional requirements

Use the ISO/IEC 25010 quality characteristics as a checklist: functional suitability,
performance efficiency, compatibility, interaction capability (usability,
accessibility), reliability, security, maintainability, flexibility (portability),
safety. For each relevant one, make it measurable:

- **Scale**: what is measured (e.g. 95th percentile response time of search).
- **Meter**: how it is measured (load profile, environment, tool).
- **Threshold**: the minimum acceptable value (the requirement).
- **Target**: the desired value (a goal, "should").

## Verification method

Every requirement names how it will be verified:

- **Inspection**: examine the artefact, document or code.
- **Analysis**: calculation, model, simulation, review of evidence.
- **Demonstration**: show the function works, qualitative pass/fail.
- **Test**: controlled stimuli, measured results against criteria.

If none fits, the requirement is not verifiable yet; rewrite it.

## User stories and acceptance criteria

When the project works with stories, write `As a <role>, I want <capability>, so that
<benefit>.`, then acceptance criteria that each meet the characteristics above,
preferably as `Given <context>, when <event>, then <outcome>.` A story is a container
for requirements, not a substitute for them: the acceptance criteria are the
requirements and must be verifiable on their own.

## Traceability

- **Upward**: every requirement traces to its parent or source (why it exists).
- **Downward**: every requirement is allocated to architecture elements or child
  requirements (who satisfies it).
- **Sideways**: every requirement traces to the tests or other evidence verifying it.
- Report gaps: requirements without parent, without children where expected, without
  verification; and tests or design elements without a requirement.
- On change, walk the links both ways and list every affected item.

## Review checklist

Use this when asked to review a specification; report findings per requirement ID with
the violated rule and a proposed rewrite.

- [ ] Each requirement is singular, has a subject and uses "shall" correctly.
- [ ] No vague term, no unquantified quality, no hidden design.
- [ ] Conditions, triggers, units and tolerances are present.
- [ ] Rationale, source, priority and verification method are set.
- [ ] Terms are defined in the glossary and used consistently.
- [ ] No duplicates, no conflicts, no requirement contradicting a constraint.
- [ ] Error, boundary and degraded-mode behaviour is covered, not only the happy path.
- [ ] Every requirement traces up; no orphan without a recorded reason.
- [ ] TBDs and assumptions are marked and owned.

## Templates

`references/templates.md` holds the requirement record, a specification outline, the
review finding format and a traceability matrix layout.

## Adjacent skills

Deriving tests from requirements: `test-engineering`. Allocating requirements to system
elements, or within a software item, belongs to system and software architecture, for
which Rotor ships no skill.
