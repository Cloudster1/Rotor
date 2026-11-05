"""The run's spans (#37, #241): one tree under the run's traceparent of Rotor's spans and OpenInference's for every
chain, node, tool and model call, sent over the channel, redacted, and with text only when the run may keep it."""

from __future__ import annotations

from typing import Any

import pytest
from langchain_core.messages import AIMessage
from langchain_core.tools import BaseTool, StructuredTool
from support import ToolCallingModel
from test_compile import FakeTools, workflow

from rotor_runner.compile import RunContext, run_workflow
from rotor_runner.pb.rotor.runner.v1 import runner_pb2 as pb
from rotor_runner.spans import is_text
from rotor_runner.spec import parse
from rotor_runner.telemetry import TOOL, RunTracing

pytestmark = pytest.mark.anyio

TRACE = "0af7651916cd43dd8448eb211c80319c"
PARENT = "b7ad6b7169203331"
TRACEPARENT = f"00-{TRACE}-{PARENT}-01"
SECRET = "stub_oat_the-run-token-1234"
RUN = "run-1"
AGENT = "  - {id: a, type: agent, prompt: 'Look up project p', tools: [t]}"
CALL = AIMessage("", tool_calls=[{"name": "get_project", "args": {"project": "p"}, "id": "call-1"}])


def scrub(text: str) -> str:
    return text.replace(SECRET, "[redacted]")


class TracedTools(FakeTools):
    """FakeTools whose calls make the span McpGateway makes around an MCP call, wherever it is made from."""

    tracing: RunTracing

    async def call(self, binding: str, name: str, arguments: dict[str, Any], node: str) -> Any:
        with self.tracing.maybe(f"mcp {name}", {"mcp.tool": name}, kind=TOOL):
            return await super().call(binding, name, arguments, node)

    async def langchain_tools(self, bindings: list[str], node: str) -> list[BaseTool]:
        async def lookup(project: str) -> str:
            with self.tracing.maybe("mcp get_project", {"mcp.tool": "get_project"}, kind=TOOL):
                return f"project {project} has 3 requirements, {SECRET}"

        return [StructuredTool.from_function(coroutine=lookup, name="get_project", description="Reads a project.")]


class Spans(dict[str, pb.RunnerSpan]):
    """A run's spans by name, a name that comes twice keeping its last span, and all of them in every."""

    every: list[pb.RunnerSpan]


async def traced_run(content: bool, nodes: str, model: list[AIMessage] | None = None) -> Spans:
    sent: list[pb.ConnectRequest] = []
    tracing = RunTracing(TRACEPARENT, sent.append, scrub, content, run=RUN)
    tools = TracedTools({"whoami": {"username": "bob"}})
    tools.tracing = tracing

    async def emit(_kind: str, _node: str, _payload: dict[str, Any]) -> None:
        return None

    ctx = RunContext(
        spec=parse(workflow(nodes)),
        tools=tools,
        emit=emit,
        model=(lambda: ToolCallingModel(responses=list(model))) if model else None,
        tracing=tracing,
    )
    outcome = await run_workflow(ctx, {"note": SECRET}, thread_id=RUN)
    assert outcome.status == "succeeded", outcome
    await tracing.flush()
    every = [span for message in sent for span in message.spans.spans]
    assert {s.trace_id.hex() for s in every} == {TRACE}
    spans = Spans({s.name: s for s in every})
    spans.every = every
    return spans


def ancestors(spans: Spans, name: str) -> list[str]:
    by_id = {s.span_id: s for s in spans.every}
    out, span = [], spans[name]
    while span.parent_span_id in by_id:
        span = by_id[span.parent_span_id]
        out.append(span.name)
    return out


async def test_an_agent_run_is_one_tree_of_chain_node_tool_and_model_spans() -> None:
    spans = await traced_run(False, AGENT, [CALL, AIMessage("It has 3.")])
    kinds = {s.name: s.attributes["openinference.span.kind"] for s in spans.values()}
    model = next(name for name, kind in kinds.items() if kind == "LLM")
    assert spans["run"].parent_span_id.hex() == PARENT
    assert (kinds["run"], kinds["node a"], kinds["get_project"], kinds["mcp get_project"]) == (
        "CHAIN",
        "AGENT",
        "TOOL",
        "TOOL",
    )
    assert ancestors(spans, "mcp get_project")[:1] == ["get_project"], "an MCP call nests in the tool that made it"
    assert {"node a", "run"} <= set(ancestors(spans, "get_project"))
    assert {"node a", "run"} <= set(ancestors(spans, model)), "the model call nests in its node"
    assert ancestors(spans, "run") == []
    by_id = {s.span_id for s in spans.every}
    assert all(s.parent_span_id in by_id for s in spans.every if s.name != "run"), "one tree under the run's span"
    assert {s.attributes["rotor.run"] for s in spans.every} == {RUN}
    assert spans[model].attributes["gen_ai.request.model"] == model
    assert spans["node a"].attributes["rotor.node.type"] == "agent"


async def test_a_tool_node_s_mcp_call_nests_in_its_node() -> None:
    spans = await traced_run(False, "  - {id: a, type: tool, tool: t, call: whoami}")
    assert ancestors(spans, "mcp whoami")[0] == "node a"
    assert spans["node a"].attributes["openinference.span.kind"] == "CHAIN"
    assert spans["node a"].attributes["rotor.node.type"] == "tool"


async def test_without_content_no_span_carries_text() -> None:
    spans = await traced_run(False, AGENT, [CALL, AIMessage(f"It has 3, {SECRET}")])
    keys = {key for s in spans.every for key in s.attributes}
    assert not [key for key in keys if is_text(key)], keys
    assert not any("__REDACTED__" in v or SECRET in v for s in spans.every for v in s.attributes.values())


async def test_with_content_spans_carry_redacted_text() -> None:
    spans = await traced_run(True, AGENT, [CALL, AIMessage(f"It has 3, {SECRET}")])
    model = [s for s in spans.every if s.attributes["openinference.span.kind"] == "LLM"][-1]
    # The fixed policy (#40) leads every prompt, the author's words follow.
    assert model.attributes["llm.input_messages.0.message.content"].startswith("You are one step of a workflow")
    assert "Look up project p" in model.attributes["llm.input_messages.1.message.content"]
    assert model.attributes["llm.output_messages.0.message.content"] == "It has 3, [redacted]"
    assert "[redacted]" in spans["get_project"].attributes["output.value"]
    assert not any(SECRET in v for s in spans.every for v in s.attributes.values()), "the runner redacts spans"


async def test_without_a_sampled_traceparent_there_are_no_spans() -> None:
    sent: list[pb.ConnectRequest] = []
    for traceparent in ("", f"00-{TRACE}-{PARENT}-00"):
        tracing = RunTracing(traceparent, sent.append, scrub)
        assert not tracing.on
        assert tracing.callbacks() == []
        with tracing.root("run"):
            pass
        await tracing.flush()
    assert sent == []
