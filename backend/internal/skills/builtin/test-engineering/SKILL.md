---
name: test-engineering
description: Test engineering method covering test strategy and planning, risk-based prioritisation, test levels from unit to acceptance, deriving test cases from requirements and designs with black-box and white-box techniques, non-functional testing, test automation quality, flaky test diagnosis, defect reports and test completion reporting with requirements coverage. Use whenever the task is to plan or review testing, design or write test cases or automated tests, decide what and how much to test, verify requirements, chase a flaky or failing test, or report test results and residual risk.
license: Apache-2.0
metadata:
  version: "1"
---

# Test engineering

Provide evidence, at the cheapest level that can produce it, that the product meets its
requirements and that the risks that matter are under control. Grounded in ISO/IEC/IEEE
29119 and the V-model pairing of design levels with test levels.

Project conventions win: use the repository's test layers, frameworks, naming, helpers
and gate scripts. Find them before writing a single test.

## Start from what exists

1. Find how the project runs its tests (README, contributor guide, Makefile, CI
   config, test scripts) and run the relevant layer before changing anything, so you
   know the baseline.
2. Read neighbouring tests and copy their structure, fixtures and helpers.
3. Learn the project's rules on mocking, test data, shared environments and ordering,
   and follow them.

## Test levels

| Level | Verifies against | Typical scope |
|---|---|---|
| Unit | detailed design, unit contracts | one function, class or module, in-process |
| Integration | software architecture, interfaces | components together, real database, real contracts |
| System | system or software requirements | the whole item, black box, deployed stack |
| Acceptance | stakeholder requirements, acceptance criteria | user scenarios in a realistic environment |

Each requirement is verified at the lowest level that can genuinely decide it. Push
logic tests down; keep the higher levels for what only they can show (wiring,
contracts, end-to-end behaviour, real infrastructure). Keep the pyramid: many fast
unit tests, fewer integration tests, few end-to-end tests.

Contract tests sit between levels: they keep stand-ins and real implementations of an
interface honest against the same specification.

## Workflow

1. **Analyse the test basis.** Requirements, acceptance criteria, architecture,
   interfaces, risk register, defect history. Report requirements that cannot be
   tested as written back to `requirements-engineering` instead of guessing intent.
2. **Assess risk.** For each feature or requirement: likelihood of failure
   (complexity, novelty, change rate, defect history) times impact (users, money,
   safety, security, data). Risk decides depth, technique and order.
3. **Strategy.** Levels, test types, techniques, environments, test data, tooling,
   entry and exit criteria, who does what. Write it down for anything bigger than a
   single change (see templates).
4. **Design test cases** with the techniques below; each case traces to the
   requirement or risk it covers.
5. **Implement** as automated tests wherever the result is decidable by a machine;
   keep manual and exploratory testing for judgement, usability and discovery.
6. **Execute and analyse.** Every failure is either a product defect, a test defect
   or an environment problem; find out which before re-running.
7. **Report.** Requirements coverage, results, open defects by severity, what was not
   tested and why, residual risk.

## Test design techniques

Black box, from the specification:

- **Equivalence partitioning**: one case per valid and per invalid class of input.
- **Boundary value analysis**: at, just below and just above every boundary.
- **Decision tables**: every relevant combination of conditions to actions.
- **State transition**: every state, every valid transition, and invalid transitions
  from each state.
- **Pairwise / combinatorial**: cover all pairs of parameter values when the full
  product is too large.
- **Scenario / use case**: main flow, alternative flows, exception flows.
- **Error guessing and fault attacks**: empty, null, huge, unicode, duplicate,
  concurrent, out of order, expired, unauthorised, injection.

White box, from the code:

- Statement and branch coverage as a gap finder, not a goal; MC/DC where the domain
  standard demands it.
- Path through error handling and cleanup code, which coverage often shows untested.

Further:

- **Property-based testing** for invariants over many generated inputs.
- **Mutation testing** to check that the tests detect changes to the code.
- **Fuzzing** for parsers, decoders and anything facing untrusted input.
- **Exploratory testing** in time-boxed sessions with a charter and notes.

## Non-functional testing

Derive from the measurable quality requirements: performance (load, stress, soak,
spike, with a defined profile and percentiles), reliability and recovery (kill,
restart, network faults, disk full), security (authorisation matrix per role and
resource, input validation, dependency scanning, secrets), accessibility (automated
checks plus keyboard and screen reader passes), usability, compatibility,
installability and upgrade or migration.

For an access control rule, test both that the allowed actor succeeds and that every
other actor is refused, at the service and, where the rule is enforced there, directly
at the database or storage layer as well.

## Qualities of a good automated test

- Tests behaviour through a public interface, not implementation details.
- Deterministic: controlled time, randomness, ordering and external state.
- Independent: creates what it needs, cleans up or isolates, never relies on another
  test or on totals in a shared environment.
- One reason to fail, and its name says which behaviour broke.
- Arrange, act, assert; assertions are specific and their messages helpful.
- Fast enough to run on every change at its level.
- Fakes or stubs only at boundaries the test does not own; nothing below the layer
  under test is mocked if the real thing can run.
- For a bug fix: write the test first, watch it fail for the right reason, then fix.
  A test that passes without the fix proves nothing.

## Flaky tests

Never mask with retries or longer sleeps. Reproduce by running it repeatedly, in
isolation and with the suite, then find the cause: shared state, ordering, time,
concurrency, async waits on fixed delays instead of conditions, resource exhaustion,
environment drift. Fix the cause or quarantine with a tracked issue.

## Defect report

Title stating the wrong behaviour; environment and version; steps to reproduce;
expected (with the requirement ID) versus actual; evidence (logs, screenshots,
request IDs); severity (impact) and priority (urgency) kept separate; minimal
reproduction if possible.

## Review checklist

- [ ] Every requirement in scope has at least one test or other verification evidence.
- [ ] Depth follows risk; the highest risks have the most and earliest tests.
- [ ] Boundaries, invalid input, error paths and permissions are covered.
- [ ] Each test is at the lowest level that can decide it.
- [ ] Tests are deterministic, independent and named by behaviour.
- [ ] No mocking of what the test is meant to verify.
- [ ] Bug fixes come with a test that failed before the fix.
- [ ] The report states what was not tested and the residual risk.

## Templates

`references/templates.md` holds the test strategy outline, the test case record, the
risk matrix and the completion report.

## Adjacent skills

Making requirements testable: `requirements-engineering`. Integration order and
interfaces come from the system and software architecture, for which Rotor ships no
skill.
