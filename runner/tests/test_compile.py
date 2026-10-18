"""The compiler, one test per node type, plus limits and tool filtering, with a fake model and fake tools."""

from __future__ import annotations

import asyncio
from collections.abc import Sequence
from typing import Any

import pytest
from langchain_core.messages import AIMessage, BaseMessage
from langchain_core.tools import BaseTool, StructuredTool
from langgraph.checkpoint.memory import InMemorySaver
from support import ToolCallingModel

from rotor_runner.compile import Outcome, RunContext, render, run_workflow
from rotor_runner.mcp import Binding, ToolFailedError, ToolRefusedError, allowed
from rotor_runner.spec import parse

pytestmark = pytest.mark.anyio


class FakeTools:
    """Tools that answer from a table, refusing what a binding does not allow, as McpGateway does."""

    def __init__(self, answers: dict[str, Any], delay: float = 0) -> None:
        self.answers, self.delay = answers, delay
        self.calls: list[tuple[str, str, dict[str, Any]]] = []

    async def call(self, binding: str, name: str, arguments: dict[str, Any], node: str) -> Any:
        self.calls.append((binding, name, arguments))
        await asyncio.sleep(self.delay)
        if name == "forbidden":
            raise ToolRefusedError(f"{binding} does not allow {name}")
        if name == "broken":
            raise ToolFailedError("the tool answered an error")
        return self.answers[name]

    async def langchain_tools(self, bindings: list[str], node: str) -> list[BaseTool]:
        def lookup(project: str) -> str:
            self.calls.append(("agent", "get_project", {"project": project}))
            return f"project {project} has 3 requirements"

        return [StructuredTool.from_function(lookup, name="get_project", description="Reads a project.")]


def workflow(nodes: str, edges: str = "", tools: str = "", limits: str = "maxSteps: 20, timeout: 1m") -> str:
    binding = "tools:\n  t: {tool: exciter, scopes: [mcp], allowTools: [get_project, whoami, forbidden, broken]}\n"
    return (
        f"apiVersion: rotor.dev/v1\nkind: Workflow\nmetadata: {{name: test}}\n{tools or binding}"
        f"limits: {{{limits}}}\nentry: a\nnodes:\n{nodes}\nedges:\n{edges or '  []'}\n"
    ).replace("edges:\n  []", "edges: []")


async def events_of(
    yaml: str,
    inputs: dict[str, Any],
    tools: FakeTools | None = None,
    model: Sequence[BaseMessage] | None = None,
    **kwargs: Any,
) -> tuple[Outcome, list[tuple[str, str, dict[str, Any]]]]:
    seen: list[tuple[str, str, dict[str, Any]]] = []

    async def emit(kind: str, node: str, payload: dict[str, Any]) -> None:
        seen.append((kind, node, payload))

    ctx = RunContext(
        spec=parse(yaml),
        tools=tools or FakeTools({}),
        emit=emit,
        model=(lambda: ToolCallingModel(responses=list(model))) if model else None,
    )
    outcome = await run_workflow(ctx, inputs, thread_id="t-1", **kwargs)
    return outcome, seen


def test_templates_fill_in_values_and_keep_types() -> None:
    state = {"inputs": {"project": "RTR", "count": 3}, "nodes": {"draft": {"title": "Login"}}}
    assert render(
        {
            "p": "{{ inputs.project }}",
            "n": "{{ inputs.count }}",
            "t": "About {{ nodes.draft.title }}",
            "x": ["{{ nodes.missing.y }}"],
        },
        state,
    ) == {"p": "RTR", "n": 3, "t": "About Login", "x": [None]}


async def test_a_tool_node_calls_with_rendered_arguments_and_keeps_the_answer() -> None:
    tools = FakeTools({"get_project": {"key": "RTR", "requirements": 3}})
    outcome, seen = await events_of(
        workflow("  - {id: a, type: tool, tool: t, call: get_project, arguments: {key: '{{ inputs.project }}'}}"),
        {"project": "RTR"},
        tools,
    )
    assert outcome.status == "succeeded"
    assert outcome.output == {"a": {"key": "RTR", "requirements": 3}}
    assert tools.calls == [("t", "get_project", {"key": "RTR"})]
    assert [(k, n) for k, n, _ in seen] == [("node.started", "a"), ("node.finished", "a")]
    assert seen[1][2]["ok"] is True


async def test_a_refused_or_failed_call_ends_the_run_saying_why() -> None:
    for call, reason in (("forbidden", "tool_refused"), ("broken", "tool_error")):
        outcome, _ = await events_of(workflow(f"  - {{id: a, type: tool, tool: t, call: {call}}}"), {})
        assert (outcome.status, outcome.reason) == ("failed", reason)


async def test_a_router_follows_the_value_or_its_default() -> None:
    nodes = (
        "  - {id: a, type: router, on: inputs.kind, cases: {bug: b, 'true': c}, default: d}\n"
        "  - {id: b, type: tool, tool: t, call: whoami}\n  - {id: c, type: tool, tool: t, call: whoami}\n"
        "  - {id: d, type: tool, tool: t, call: whoami}"
    )
    for kind, expected in (("bug", "b"), (True, "c"), ("feature", "d")):
        outcome, _ = await events_of(workflow(nodes), {"kind": kind}, FakeTools({"whoami": {"username": "bob"}}))
        assert list(outcome.output) == [expected], kind


async def test_a_router_without_a_case_or_default_fails() -> None:
    nodes = (
        "  - {id: a, type: router, on: inputs.kind, cases: {bug: b}}\n  - {id: b, type: tool, tool: t, call: whoami}"
    )
    outcome, _ = await events_of(workflow(nodes), {"kind": "feature"})
    assert (outcome.status, outcome.reason) == ("failed", "no_route")


async def test_an_approval_waits_and_the_answer_resumes_or_cancels_the_run() -> None:
    nodes = (
        "  - {id: a, type: approval, message: 'File {{ inputs.title }}?'}\n"
        "  - {id: b, type: tool, tool: t, call: whoami}"
    )
    yaml, saver = workflow(nodes, "  - {from: a, to: b}"), InMemorySaver()
    tools = FakeTools({"whoami": {"username": "bob"}})
    outcome, seen = await events_of(yaml, {"title": "Login"}, tools, checkpointer=saver)
    assert outcome.status == "interrupted"
    assert outcome.interrupt == {"node": "a", "message": "File Login?"}
    assert ("interrupt", "a", {"message": "File Login?"}) in seen
    assert tools.calls == []

    outcome, _ = await events_of(yaml, {}, tools, checkpointer=saver, resuming=True, resume={"approved": True})
    assert outcome.status == "succeeded"
    assert outcome.output["a"] == {"approved": True}
    assert tools.calls == [("t", "whoami", {})]

    saver = InMemorySaver()
    await events_of(yaml, {"title": "Login"}, tools, checkpointer=saver)
    outcome, _ = await events_of(yaml, {}, tools, checkpointer=saver, resuming=True, resume={"approved": False})
    assert (outcome.status, outcome.reason) == ("cancelled", "rejected")


async def test_an_agent_calls_its_tools_and_answers_in_its_schema() -> None:
    model = [
        AIMessage("", tool_calls=[{"name": "get_project", "args": {"project": "RTR"}, "id": "c1"}]),
        AIMessage(
            "",
            tool_calls=[
                {"name": "submit_output", "args": {"title": "Login", "statement": "Users sign in."}, "id": "c2"}
            ],
        ),
    ]
    tools = FakeTools({})
    nodes = (
        "  - id: a\n    type: agent\n    prompt: Draft for {{ inputs.project }}\n    tools: [t]\n"
        "    output: {schema: {type: object, required: [title, statement]}}"
    )
    outcome, _ = await events_of(workflow(nodes), {"project": "RTR"}, tools, model)
    assert outcome.status == "succeeded", outcome
    assert outcome.output["a"] == {"title": "Login", "statement": "Users sign in."}
    assert ("agent", "get_project", {"project": "RTR"}) in tools.calls


async def test_an_agent_without_a_schema_answers_text_and_one_breaking_its_schema_fails() -> None:
    outcome, _ = await events_of(
        workflow("  - {id: a, type: agent, prompt: Say hello}"), {}, model=[AIMessage("Hello there")]
    )
    assert outcome.output == {"a": {"text": "Hello there"}}
    bad = [AIMessage("", tool_calls=[{"name": "submit_output", "args": {"title": 7}, "id": "c1"}])]
    nodes = "  - {id: a, type: agent, prompt: Draft, output: {schema: {type: object, required: [statement]}}}"
    outcome, _ = await events_of(workflow(nodes), {}, model=bad)
    assert (outcome.status, outcome.reason) == ("failed", "output_schema")


async def test_an_agent_without_a_model_fails_saying_so() -> None:
    outcome, _ = await events_of(workflow("  - {id: a, type: agent, prompt: Hi}"), {})
    assert (outcome.status, outcome.reason) == ("failed", "model_missing")


async def test_limits_end_a_run_with_their_reason() -> None:
    loop = "  - {id: a, type: tool, tool: t, call: whoami}\n  - {id: b, type: tool, tool: t, call: whoami}"
    tools = FakeTools({"whoami": {"username": "bob"}})
    outcome, _ = await events_of(
        workflow(loop, "  - {from: a, to: b}\n  - {from: b, to: a}", limits="maxSteps: 3, timeout: 1m"), {}, tools
    )
    assert (outcome.status, outcome.reason) == ("failed", "max_steps")

    slow = FakeTools({"whoami": {}}, delay=3)
    outcome, _ = await events_of(
        workflow("  - {id: a, type: tool, tool: t, call: whoami}", limits="maxSteps: 5, timeout: 1s"), {}, slow
    )
    assert (outcome.status, outcome.reason) == ("failed", "timeout")

    hungry = [
        AIMessage("A long answer", usage_metadata={"input_tokens": 400, "output_tokens": 200, "total_tokens": 600})
    ]
    outcome, _ = await events_of(
        workflow("  - {id: a, type: agent, prompt: Hi}", limits="maxSteps: 5, timeout: 1m, maxTokens: 100"),
        {},
        model=hungry,
    )
    assert (outcome.status, outcome.reason) == ("failed", "max_tokens")


async def test_a_code_node_needs_a_code_runner() -> None:
    outcome, _ = await events_of(
        workflow("  - {id: a, type: code, source: 'def run(s, c): return {}', output: {schema: {type: object}}}"), {}
    )
    assert (outcome.status, outcome.reason) == ("failed", "code_unavailable")


def test_a_binding_offers_only_its_allowed_tools_and_reading_ones_unless_it_writes() -> None:
    read = Binding("r", "http://t/mcp", ("mcp:read",), "read", ("get_project", "create_requirement"))
    write = Binding("w", "http://t/mcp", ("mcp",), "write", ("create_requirement",))
    assert allowed(read, "get_project", {"readOnlyHint": True})
    assert not allowed(read, "create_requirement", {"readOnlyHint": False})
    assert not allowed(read, "get_project", None)
    assert not allowed(read, "delete_everything", {"readOnlyHint": True})
    assert allowed(write, "create_requirement", {"destructiveHint": False})
    assert not allowed(write, "get_project", {"readOnlyHint": True})
