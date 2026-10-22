"""Bounded loops (#134): visits counted in the run's state end a loop on maxVisits and survive a resume."""

from __future__ import annotations

from typing import Any

import pytest
from test_checkpoint import ControlPlane
from test_compile import FakeTools, events_of, workflow

from rotor_runner.checkpoint import RemoteSaver
from rotor_runner.compile import RunContext, run_workflow
from rotor_runner.spec import parse

pytestmark = pytest.mark.anyio


class CountingTools(FakeTools):
    """Tools whose every answer says how many calls came before, so a node's output tells its visits apart."""

    async def call(self, binding: str, name: str, arguments: dict[str, Any], node: str) -> Any:
        self.calls.append((binding, name, arguments))
        return {"call": len(self.calls)}


# a goes round itself through the router until it has no visit left, then the run goes on at out.
LOOP = (
    "  - {id: a, type: tool, tool: t, call: whoami, maxVisits: 3, onExhausted: out}\n"
    "  - {id: again, type: router, on: visits.a, cases: {'9': out}, default: a}\n"
    "  - id: out\n    type: tool\n    tool: t\n    call: get_project\n"
    "    arguments: {visits: '{{ visits.a }}', last: '{{ nodes.a }}'}"
)


def started(seen: list[tuple[str, str, dict[str, Any]]], node: str) -> list[int]:
    return [p["visit"] for k, n, p in seen if k == "node.started" and n == node]


async def test_a_loop_ends_on_its_count_and_follows_on_exhausted() -> None:
    tools = CountingTools({})
    outcome, seen = await events_of(workflow(LOOP, "  - {from: a, to: again}"), {}, tools)
    assert outcome.status == "succeeded", outcome
    assert [c[1] for c in tools.calls] == ["whoami"] * 3 + ["get_project"]
    assert tools.calls[-1][2] == {"visits": 3, "last": {"call": 3}}, "templates read the count and the latest output"
    assert outcome.output["a"] == {"call": 3}
    assert started(seen, "a") == [1, 2, 3]
    assert started(seen, "again") == [1, 2, 3]
    assert started(seen, "out") == [1]
    finished = [p for k, n, p in seen if k == "node.finished" and n == "a"]
    assert finished[-1]["outputSummary"] == '{"nodes": {"a": {"call": 3}}}', "the visit is not part of the output"


async def test_a_loop_with_nowhere_to_go_ends_the_run_as_loop_exhausted() -> None:
    nodes = "  - {id: a, type: tool, tool: t, call: whoami, maxVisits: 2}"
    tools = CountingTools({})
    outcome, _ = await events_of(workflow(nodes, "  - {from: a, to: a}"), {}, tools)
    assert (outcome.status, outcome.reason) == ("failed", "loop_exhausted")
    assert "a ran 2 times" in outcome.output["message"]
    assert len(tools.calls) == 2

    # onExhausted nodes with no visit left either end the run the same way, rather than go round.
    nodes = (
        "  - {id: a, type: tool, tool: t, call: whoami, maxVisits: 1, onExhausted: b}\n"
        "  - {id: b, type: tool, tool: t, call: whoami, maxVisits: 1, onExhausted: a}"
    )
    outcome, _ = await events_of(workflow(nodes, "  - {from: a, to: b}\n  - {from: b, to: a}"), {}, CountingTools({}))
    assert (outcome.status, outcome.reason) == ("failed", "loop_exhausted")


async def test_a_router_reads_how_often_a_node_ran() -> None:
    nodes = (
        "  - {id: a, type: tool, tool: t, call: whoami}\n"
        "  - {id: again, type: router, on: visits.a, cases: {'2': done}, default: a}\n"
        "  - {id: done, type: tool, tool: t, call: get_project}"
    )
    tools = CountingTools({})
    outcome, _ = await events_of(workflow(nodes, "  - {from: a, to: again}"), {}, tools)
    assert outcome.status == "succeeded"
    assert [c[1] for c in tools.calls] == ["whoami", "whoami", "get_project"]


# An approval that asks again until it has been answered twice, then the run goes on at done.
ASK_TWICE = """apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: twice}
tools:
  t: {tool: exciter, scopes: [mcp], allowTools: [whoami]}
limits: {maxSteps: 20, timeout: 1m}
entry: ask
nodes:
  - {id: ask, type: approval, message: 'Asked {{ visits.ask }} times before. Go on?', maxVisits: 2, onExhausted: done}
  - {id: done, type: tool, tool: t, call: whoami}
edges:
  - {from: ask, to: ask}
"""


async def test_the_count_lives_in_the_checkpoint_and_survives_an_approval_resume() -> None:
    plane, tools = ControlPlane(), CountingTools({})
    seen: list[tuple[str, str, dict[str, Any]]] = []

    async def emit(kind: str, node: str, payload: dict[str, Any]) -> None:
        seen.append((kind, node, payload))

    async def run(**kwargs: Any) -> Any:
        # A fresh saver and graph each time, as in a new sandbox: only the control plane remembers the run.
        ctx = RunContext(spec=parse(ASK_TWICE), tools=tools, emit=emit)
        return await run_workflow(ctx, {}, thread_id="run-1", checkpointer=RemoteSaver(plane), **kwargs)  # type: ignore[arg-type]

    first = await run()
    assert first.interrupt is not None and first.interrupt["message"] == "Asked 0 times before. Go on?"
    second = await run(resuming=True, resume={"approved": True}, resume_id=first.interrupt["id"])
    assert second.interrupt is not None and second.interrupt["message"] == "Asked 1 times before. Go on?"
    assert started(seen, "ask") == [1, 1, 2], "the resumed visit keeps its number, the next one counts on"
    third = await run(resuming=True, resume={"approved": True}, resume_id=second.interrupt["id"])
    assert third.status == "succeeded", third
    assert started(seen, "done") == [1]
    assert len(tools.calls) == 1
