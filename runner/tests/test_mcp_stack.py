"""The runner against the stack's tool double over real MCP: calls carry the token, and bindings narrow the tools.
Run by make test-runner, inside the stack's network; check-python skips them."""

from __future__ import annotations

import os
import uuid
from typing import Any

import httpx
import pytest
from langchain_core.messages import AIMessage
from support import ToolCallingModel

from rotor_runner.compile import RunContext, run_workflow
from rotor_runner.mcp import Binding, McpGateway, ToolRefusedError
from rotor_runner.spec import parse

pytestmark = [pytest.mark.stack, pytest.mark.anyio]

STUB = os.environ.get("ROTOR_TEST_TOOLSTUB_INTERNAL_URL", "")
CONTROL = os.environ.get("ROTOR_TEST_TOOLSTUB_CONTROL_URL", "")


class Tokens:
    """Bob's personal token at a tenant without OAuth, as the control plane would grant a run's token."""

    def __init__(self) -> None:
        self.asked: list[str] = []

    async def token(self, binding: str) -> str:
        self.asked.append(binding)
        return "stub_pat_bob"


@pytest.fixture
def tenant() -> str:
    if not STUB or not CONTROL:
        pytest.skip("run by make test-runner, inside the stack's network")
    name = "runner-" + uuid.uuid4().hex[:12]
    answer = httpx.post(f"{CONTROL}/control/tenants", json={"name": name, "oauth": False})
    assert answer.status_code == 201, answer.text
    return f"{STUB}/t/{name}/api/v1/mcp"


def gateway(url: str, access: str, allow: tuple[str, ...], events: list[Any]) -> McpGateway:
    async def emit(kind: str, node: str, payload: dict[str, Any]) -> None:
        events.append((kind, node, payload))

    binding = Binding("exciter", url, ("mcp",), access, allow)
    return McpGateway(bindings={"exciter": binding}, tokens=Tokens(), emit=emit)


async def test_a_tool_node_calls_the_tool_as_the_person(tenant: str) -> None:
    events: list[Any] = []
    yaml = """apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: who}
tools:
  exciter: {tool: exciter, scopes: [mcp:read], allowTools: [whoami]}
limits: {maxSteps: 5, timeout: 1m}
entry: ask
nodes:
  - {id: ask, type: tool, tool: exciter, call: whoami}
"""
    gw = gateway(tenant, "read", ("whoami",), events)
    outcome = await run_workflow(RunContext(spec=parse(yaml), tools=gw, emit=gw.emit), {}, thread_id="t")
    assert outcome.status == "succeeded", outcome
    assert outcome.output["ask"]["user"]["name"] == "bob"
    called = [p for kind, _, p in events if kind == "tool.called"]
    assert called and called[0]["tool"] == "whoami" and called[0]["ok"] is True


async def test_a_read_binding_offers_and_allows_only_reading_tools(tenant: str) -> None:
    gw = gateway(tenant, "read", ("whoami", "get_project", "create_requirement"), [])
    offered = sorted(t.name for t in await gw.langchain_tools(["exciter"], "agent"))
    assert offered == ["get_project", "whoami"]
    with pytest.raises(ToolRefusedError):
        await gw.call("exciter", "create_requirement", {"title": "x"}, "n")
    with pytest.raises(ToolRefusedError):
        await gw.call("exciter", "list_projects", {}, "n")


async def test_a_write_binding_offers_what_it_allows(tenant: str) -> None:
    gw = gateway(tenant, "write", ("create_requirement",), [])
    assert [t.name for t in await gw.langchain_tools(["exciter"], "agent")] == ["create_requirement"]


async def test_an_agent_calls_the_tool_through_mcp_with_the_token(tenant: str) -> None:
    events: list[Any] = []
    gw = gateway(tenant, "read", ("whoami",), events)
    yaml = """apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: agent}
tools:
  exciter: {tool: exciter, scopes: [mcp:read], allowTools: [whoami]}
limits: {maxSteps: 10, timeout: 1m}
entry: a
nodes:
  - {id: a, type: agent, prompt: Who am I?, tools: [exciter]}
"""
    script = [AIMessage("", tool_calls=[{"name": "whoami", "args": {}, "id": "c1"}]), AIMessage("You are bob.")]
    ctx = RunContext(spec=parse(yaml), tools=gw, emit=gw.emit, model=lambda: ToolCallingModel(responses=list(script)))
    outcome = await run_workflow(ctx, {}, thread_id="t")
    assert outcome.status == "succeeded", outcome
    assert outcome.output["a"] == {"text": "You are bob."}
    called = [p for kind, _, p in events if kind == "tool.called"]
    assert called and called[0]["tool"] == "whoami" and "bob" in called[0]["resultSummary"]
