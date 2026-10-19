"""The run's spans (#37): one trace under the run's traceparent, sent over the channel, redacted, and no text from the
model unless the run may carry it for LangSmith."""

from __future__ import annotations

from typing import Any

import pytest
from langchain_core.messages import AIMessage
from support import ToolCallingModel
from test_compile import FakeTools, workflow

from rotor_runner.compile import RunContext, run_workflow
from rotor_runner.pb.rotor.runner.v1 import runner_pb2 as pb
from rotor_runner.spec import parse
from rotor_runner.telemetry import RunTracing

pytestmark = pytest.mark.anyio

TRACE = "0af7651916cd43dd8448eb211c80319c"
PARENT = "b7ad6b7169203331"
TRACEPARENT = f"00-{TRACE}-{PARENT}-01"
SECRET = "stub_oat_the-run-token-1234"


def scrub(text: str) -> str:
    return text.replace(SECRET, "[redacted]")


async def traced_run(content: bool, model: list[AIMessage] | None = None, nodes: str = "") -> list[pb.RunnerSpan]:
    sent: list[pb.ConnectRequest] = []
    tracing = RunTracing(TRACEPARENT, sent.append, scrub, content)

    async def emit(_kind: str, _node: str, _payload: dict[str, Any]) -> None:
        return None

    ctx = RunContext(
        spec=parse(workflow(nodes or "  - {id: a, type: tool, tool: t, call: whoami}")),
        tools=FakeTools({"whoami": {"username": "bob"}}),
        emit=emit,
        model=(lambda: ToolCallingModel(responses=list(model))) if model else None,
        tracing=tracing,
    )
    outcome = await run_workflow(ctx, {"note": SECRET}, thread_id="run-1")
    assert outcome.status == "succeeded", outcome
    await tracing.flush()
    return [span for message in sent for span in message.spans.spans]


async def test_a_run_is_one_trace_under_its_traceparent_sent_over_the_channel() -> None:
    spans = await traced_run(content=False)
    by_name = {s.name: s for s in spans}
    assert {"run", "node a"} <= set(by_name), list(by_name)
    assert {s.trace_id.hex() for s in spans} == {TRACE}
    assert by_name["run"].parent_span_id.hex() == PARENT
    assert by_name["node a"].parent_span_id == by_name["run"].span_id
    assert by_name["node a"].attributes["rotor.node.type"] == "tool"


async def test_a_model_call_has_a_span_with_its_model_and_text_only_when_allowed() -> None:
    nodes = "  - {id: a, type: agent, prompt: 'Say hello'}"
    model = [AIMessage("Hello there")]
    plain = {s.name.split(" ")[0]: s for s in await traced_run(False, model, nodes)}
    assert "llm" in plain, list(plain)
    assert "gen_ai.request.model" in plain["llm"].attributes
    assert not any(
        k.startswith("gen_ai.prompt.") or k.startswith("gen_ai.completion.") for k in plain["llm"].attributes
    )

    told = {s.name.split(" ")[0]: s for s in await traced_run(True, [AIMessage(f"Hello {SECRET}")], nodes)}
    attributes = told["llm"].attributes
    # The fixed policy (#40) leads every prompt, the author's words follow.
    assert str(attributes["gen_ai.prompt.0.content"]).startswith("You are one step of a workflow")
    assert attributes["gen_ai.prompt.1.content"] == "Say hello"
    assert attributes["gen_ai.completion.0.content"] == "Hello [redacted]", "the runner's redactor runs over spans"


async def test_without_a_sampled_traceparent_there_are_no_spans() -> None:
    sent: list[pb.ConnectRequest] = []
    for traceparent in ("", f"00-{TRACE}-{PARENT}-00"):
        tracing = RunTracing(traceparent, sent.append, scrub)
        assert not tracing.on
        with tracing.root("run"):
            pass
        await tracing.flush()
    assert sent == []
