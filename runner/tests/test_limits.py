"""The runner's own limits (#38): the rate of tool calls, which ends a run even inside an agent, and the usage
each model call reports for the control plane to total."""

from __future__ import annotations

from typing import Any

import pytest
from langchain_core.messages import AIMessage
from langchain_core.tools import BaseTool, StructuredTool
from test_compile import FakeTools, events_of, workflow

from rotor_runner.limits import RunEnded, ToolRate, ToolRateExceeded

pytestmark = pytest.mark.anyio


def test_the_rate_counts_calls_in_a_sliding_minute() -> None:
    now = [0.0]
    rate = ToolRate(2, clock=lambda: now[0])
    rate.check()
    now[0] = 30
    rate.check()
    with pytest.raises(ToolRateExceeded) as raised:
        rate.check()
    assert isinstance(raised.value, RunEnded) and raised.value.reason == "tool_rate"
    now[0] = 61
    rate.check()


class LimitedTools(FakeTools):
    """An agent's tool that hits the run's rate on its second call, as McpGateway's interceptor would."""

    def __init__(self) -> None:
        super().__init__({})
        self.rate = ToolRate(1)

    async def langchain_tools(self, bindings: list[str], node: str) -> list[BaseTool]:
        def lookup(project: str) -> str:
            self.rate.check()
            self.calls.append(("agent", "get_project", {"project": project}))
            return f"project {project}"

        return [StructuredTool.from_function(lookup, name="get_project", description="Reads a project.")]


async def test_a_rate_past_its_limit_inside_an_agent_ends_the_run() -> None:
    calls: list[Any] = [
        AIMessage("", tool_calls=[{"name": "get_project", "args": {"project": "A"}, "id": f"c{i}"}]) for i in range(3)
    ]
    tools = LimitedTools()
    outcome, _ = await events_of(workflow("  - {id: a, type: agent, prompt: Look, tools: [t]}"), {}, tools, calls)
    assert (outcome.status, outcome.reason) == ("failed", "tool_rate"), outcome
    assert len(tools.calls) == 1


async def test_every_model_call_reports_its_usage_under_its_node() -> None:
    answer = AIMessage(
        "Hello", usage_metadata={"input_tokens": 7, "output_tokens": 3, "total_tokens": 10}, response_metadata={}
    )
    outcome, seen = await events_of(workflow("  - {id: a, type: agent, prompt: Say hello}"), {}, model=[answer])
    assert outcome.status == "succeeded", outcome
    usage = [payload for kind, _node, payload in seen if kind == "llm.usage"]
    assert usage == [{"model": "", "inputTokens": 7, "outputTokens": 3}]
    assert [node for kind, node, _ in seen if kind == "llm.usage"] == ["a"]
