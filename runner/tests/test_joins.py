"""Parallel branches (#135): a join runs once after every branch, and approvals reached together wait together."""

from __future__ import annotations

from typing import Any

import pytest
from test_checkpoint import ControlPlane
from test_compile import events_of, workflow
from test_loops import CountingTools, started

from rotor_runner.checkpoint import RemoteSaver
from rotor_runner.compile import Outcome, RunContext, run_workflow
from rotor_runner.spec import parse

pytestmark = pytest.mark.anyio

# a fans out to b and to c, which takes one step more through c2; both lead to j.
FAN = (
    "  - {id: a, type: tool, tool: t, call: whoami}\n"
    "  - {id: b, type: tool, tool: t, call: whoami}\n"
    "  - {id: c, type: tool, tool: t, call: whoami}\n"
    "  - {id: c2, type: tool, tool: t, call: whoami}\n"
    "  - {id: j, type: tool, tool: t, call: get_project, arguments: {b: '{{ nodes.b }}', c2: '{{ nodes.c2 }}'}JOIN}"
)
FAN_EDGES = (
    "  - {from: a, to: b}\n  - {from: a, to: c}\n  - {from: c, to: c2}\n  - {from: b, to: j}\n  - {from: c2, to: j}"
)


async def test_a_join_runs_once_after_branches_of_different_lengths() -> None:
    tools = CountingTools({})
    outcome, seen = await events_of(workflow(FAN.replace("JOIN", ", join: all"), FAN_EDGES), {}, tools)
    assert outcome.status == "succeeded", outcome
    assert started(seen, "j") == [1]
    assert [c[1] for c in tools.calls] == ["whoami"] * 4 + ["get_project"], "j runs last, once"
    assert tools.calls[-1][2] == {"b": outcome.output["b"], "c2": outcome.output["c2"]}, "j reads both branches"


async def test_without_a_join_a_node_runs_once_per_arriving_branch() -> None:
    tools = CountingTools({})
    outcome, seen = await events_of(workflow(FAN.replace("JOIN", ""), FAN_EDGES), {}, tools)
    assert outcome.status == "succeeded", outcome
    assert started(seen, "j") == [1, 2]


async def test_a_join_in_a_loop_waits_each_pass_and_goes_to_on_exhausted_once() -> None:
    nodes = (
        FAN.replace("JOIN", ", join: all, maxVisits: 2, onExhausted: out")
        + "\n  - {id: again, type: router, on: visits.j, cases: {'9': out}, default: a}"
        + "\n  - {id: out, type: tool, tool: t, call: whoami}"
    )
    tools = CountingTools({})
    outcome, seen = await events_of(workflow(nodes, FAN_EDGES + "\n  - {from: j, to: again}"), {}, tools)
    assert outcome.status == "succeeded", outcome
    assert started(seen, "j") == [1, 2]
    assert started(seen, "c2") == [1, 2, 3], "the third pass runs its branches, then finds j has no visit left"
    assert started(seen, "out") == [1], "both branches lead past j to out, which waits for them as j would"


async def test_max_steps_counts_the_nodes_of_every_branch() -> None:
    # Five nodes in four of LangGraph's steps: b and c run in one, which LangGraph alone would count once.
    yaml = workflow(FAN.replace("JOIN", ", join: all"), FAN_EDGES, limits="maxSteps: 5, timeout: 1m")
    outcome, _ = await events_of(yaml, {}, CountingTools({}))
    assert (outcome.status, outcome.reason) == ("failed", "max_steps"), outcome
    outcome, _ = await events_of(yaml.replace("maxSteps: 5", "maxSteps: 6"), {}, CountingTools({}))
    assert outcome.status == "succeeded", outcome


# a fans out to two approvals, which j waits for.
TWO_APPROVALS = """apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: reviews}
tools:
  t: {tool: exciter, scopes: [mcp], allowTools: [whoami]}
limits: {maxSteps: 20, timeout: 1m}
entry: a
nodes:
  - {id: a, type: tool, tool: t, call: whoami}
  - {id: x, type: approval, message: 'Code fine?'}
  - {id: y, type: approval, message: 'Security fine?'}
  - {id: j, type: tool, tool: t, call: whoami, join: all}
edges:
  - {from: a, to: x}
  - {from: a, to: y}
  - {from: x, to: j}
  - {from: y, to: j}
"""


class Sandboxes:
    """Each run in a fresh saver and graph, as in a new sandbox: only the control plane remembers the run."""

    def __init__(self) -> None:
        self.plane, self.tools = ControlPlane(), CountingTools({})
        self.seen: list[tuple[str, str, dict[str, Any]]] = []

    async def run(self, **kwargs: Any) -> Outcome:
        async def emit(kind: str, node: str, payload: dict[str, Any]) -> None:
            self.seen.append((kind, node, payload))

        ctx = RunContext(spec=parse(TWO_APPROVALS), tools=self.tools, emit=emit)
        return await run_workflow(ctx, {}, thread_id="run-1", checkpointer=RemoteSaver(self.plane), **kwargs)  # type: ignore[arg-type]


def ids(outcome: Outcome) -> dict[str, str]:
    assert outcome.status == "interrupted" and outcome.interrupt is not None, outcome
    return {w["node"]: w["id"] for w in outcome.interrupt.get("waiting", [outcome.interrupt])}


async def test_two_approvals_wait_together_and_the_run_goes_on_once_both_are_decided() -> None:
    box = Sandboxes()
    first = await box.run()
    waiting = ids(first)
    assert sorted(waiting) == ["x", "y"] and len(set(waiting.values())) == 2
    assert first.interrupt is not None and first.interrupt["id"] in waiting.values(), "the first stays where it was"
    assert sorted(n for k, n, _ in box.seen if k == "interrupt") == ["x", "y"]

    yes = {"approved": True, "decidedBy": "Alice"}
    done = await box.run(resuming=True, resume_answers={waiting["x"]: yes, waiting["y"]: {**yes, "decidedBy": "Bob"}})
    assert done.status == "succeeded", done
    assert (done.output["x"]["decidedBy"], done.output["y"]["decidedBy"]) == ("Alice", "Bob")
    assert started(box.seen, "j") == [1]
    assert len(box.tools.calls) == 2, "a ran once before the wait and j once after"


async def test_an_answer_to_one_of_two_approvals_leaves_the_run_waiting_at_the_other() -> None:
    box = Sandboxes()
    waiting = ids(await box.run())
    again = await box.run(resuming=True, resume={"approved": True}, resume_id=waiting["x"])
    assert ids(again) == {"y": waiting["y"]}
    assert started(box.seen, "j") == []


async def test_one_rejected_approval_of_two_ends_the_run() -> None:
    box = Sandboxes()
    waiting = ids(await box.run())
    answers = {waiting["x"]: {"approved": True}, waiting["y"]: {"approved": False}}
    ended = await box.run(resuming=True, resume_answers=answers)
    assert (ended.status, ended.reason) == ("cancelled", "rejected")
    assert started(box.seen, "j") == []
