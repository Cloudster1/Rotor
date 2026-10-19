"""Approvals (#31): the form and preview an approval shows, and resuming by interrupt id so no answer is replayed."""

from __future__ import annotations

from typing import Any

import pytest
from langgraph.checkpoint.memory import InMemorySaver
from test_compile import FakeTools, events_of

from rotor_runner.compile import Outcome
from rotor_runner.mcp import Binding, allowed

pytestmark = pytest.mark.anyio

BINDINGS = """tools:
  read: {tool: exciter, scopes: ['mcp:read'], allowTools: [whoami]}
  write: {tool: exciter, scopes: [mcp], access: write, allowTools: [create_requirement, whoami]}
"""


def spec(nodes: str, edges: str) -> str:
    return (
        "apiVersion: rotor.dev/v1\nkind: Workflow\nmetadata: {name: test}\n"
        + BINDINGS
        + "model: {name: m}\nlimits: {maxSteps: 30, timeout: 1m}\nentry: a\nnodes:\n"
        + nodes
        + "edges:\n"
        + edges
    )


FORM = spec(
    "  - id: a\n    type: approval\n    message: 'File {{ inputs.title }}?'\n"
    "    form:\n      schema: {type: object, properties: {title: {type: string}}}\n"
    "      values: {title: '{{ inputs.title }}'}\n"
    "  - {id: r, type: router, on: inputs.kind, cases: {bug: file}, default: agent}\n"
    "  - {id: file, type: tool, tool: write, call: create_requirement, arguments: {title: '{{ nodes.a.title }}'}}\n"
    "  - {id: look, type: tool, tool: read, call: whoami}\n"
    "  - {id: agent, type: agent, prompt: go, tools: [read, write]}\n"
    "  - {id: b, type: approval, message: Again?}\n"
    "  - {id: after, type: tool, tool: write, call: whoami}\n",
    "  - {from: a, to: r}\n  - {from: file, to: look}\n  - {from: look, to: b}\n  - {from: b, to: after}\n",
)


def without_id(outcome: Outcome) -> dict[str, Any]:
    assert outcome.interrupt is not None, outcome
    return {k: v for k, v in outcome.interrupt.items() if k != "id"}


async def test_an_approval_shows_its_form_and_the_writes_ahead_on_every_branch() -> None:
    outcome, _ = await events_of(FORM, {"title": "Login", "kind": "bug"}, checkpointer=InMemorySaver())
    shown = without_id(outcome)
    assert shown["values"] == {"title": "Login"}
    assert shown["preview"] == [
        {
            "node": "file",
            "binding": "write",
            "tool": "exciter",
            "call": "create_requirement",
            "arguments": {"title": "Login"},
        },
        {
            "node": "agent",
            "binding": "write",
            "tool": "exciter",
            "calls": ["create_requirement", "whoami"],
            "arguments": None,
        },
    ], "reads and what lies past the next approval are not shown"


async def test_the_approved_values_are_what_later_nodes_read() -> None:
    saver, tools = InMemorySaver(), FakeTools({"create_requirement": {"key": "R-1"}, "whoami": {}})
    first, _ = await events_of(FORM, {"title": "Login", "kind": "bug"}, tools, checkpointer=saver)
    assert first.interrupt is not None
    answer = {"approved": True, "values": {"title": "Sign-in"}, "comment": "renamed", "decidedBy": "carol"}
    second, _ = await events_of(
        FORM, {}, tools, checkpointer=saver, resuming=True, resume=answer, resume_id=first.interrupt["id"]
    )
    assert second.status == "interrupted" and without_id(second)["message"] == "Again?"
    assert tools.calls[0] == ("write", "create_requirement", {"title": "Sign-in"})


async def test_a_stale_answer_never_answers_the_next_approval() -> None:
    saver, tools = InMemorySaver(), FakeTools({"create_requirement": {}, "whoami": {}})
    first, _ = await events_of(FORM, {"title": "Login", "kind": "bug"}, tools, checkpointer=saver)
    assert first.interrupt is not None
    stale = {"resuming": True, "resume": {"approved": True}, "resume_id": first.interrupt["id"]}
    second, _ = await events_of(FORM, {}, tools, checkpointer=saver, **stale)
    assert second.interrupt is not None and second.interrupt["id"] != first.interrupt["id"]
    calls = len(tools.calls)

    # A sandbox replaced after the first decision is handed that decision again; b must still wait.
    third, _ = await events_of(FORM, {}, tools, checkpointer=saver, **stale)
    assert third.status == "interrupted" and third.interrupt == second.interrupt
    assert len(tools.calls) == calls, "nothing after b ran"

    fourth, _ = await events_of(
        FORM, {}, tools, checkpointer=saver, resuming=True, resume={"approved": True}, resume_id=second.interrupt["id"]
    )
    assert fourth.status == "succeeded", fourth


async def test_parallel_approvals_are_answered_one_by_one() -> None:
    yaml = spec(
        "  - {id: a, type: tool, tool: read, call: whoami}\n"
        "  - {id: x, type: approval, message: X?}\n  - {id: y, type: approval, message: Y?}\n",
        "  - {from: a, to: x}\n  - {from: a, to: y}\n",
    )
    saver, tools = InMemorySaver(), FakeTools({"whoami": {}})
    first, _ = await events_of(yaml, {}, tools, checkpointer=saver)
    assert first.interrupt is not None
    second, _ = await events_of(
        yaml, {}, tools, checkpointer=saver, resuming=True, resume={"approved": True}, resume_id=first.interrupt["id"]
    )
    assert second.interrupt is not None and second.interrupt["node"] != first.interrupt["node"]
    third, _ = await events_of(
        yaml, {}, tools, checkpointer=saver, resuming=True, resume={"approved": True}, resume_id=second.interrupt["id"]
    )
    assert third.status == "succeeded", third


def test_no_binding_offers_a_call_that_decides_a_review() -> None:
    binding = Binding("w", "http://tool", ("mcp",), "write", ("approve_review", "request_review"))
    assert not allowed(binding, "approve_review", {})
    assert allowed(binding, "request_review", {})
