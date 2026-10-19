"""The injection corpus (#40): each case plants text in what a run reads and scripts a model that does whatever the
text says, then checks what Rotor guarantees whatever the model does. Cases are tests/injection/*.yaml."""

from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

import pytest
from langchain_core.messages import AIMessage, BaseMessage, HumanMessage, SystemMessage, ToolMessage
from ruamel.yaml import YAML
from support import CorpusTools, RecordingModel

from rotor_runner.compile import RunContext, run_workflow
from rotor_runner.config import UNTRUSTED_TAG
from rotor_runner.links import LinkLimiter
from rotor_runner.mcp import Binding
from rotor_runner.policy import SYSTEM_POLICY, Fence
from rotor_runner.spec import parse

pytestmark = pytest.mark.anyio

CASES = sorted((Path(__file__).parent / "injection").glob("*.yaml"))
LIVE = re.compile(rf"<\s*(/?)\s*{UNTRUSTED_TAG}([^>]*)>", re.IGNORECASE)
FENCE_ID = "{FENCE_ID}"

DEFAULT_WORKFLOW = """apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: corpus}
tools:
  r: {tool: exciter, scopes: [mcp:read], allowTools: [get_issue, get_page, add_comment]}
  w: {tool: exciter, scopes: [mcp], access: write, allowTools: [add_comment]}
limits: {maxSteps: 30, timeout: 1m}
entry: a
nodes:
  - {id: a, type: agent, prompt: "Summarise issue {{ inputs.key }} for the team.", tools: [r, w]}
edges: []
"""
DEFAULT_HINTS = {"get_issue": {"readOnlyHint": True}, "get_page": {"readOnlyHint": True}, "add_comment": {}}
DEFAULT_HOSTS = ["exciter.example"]


def _message(step: dict[str, Any]) -> AIMessage:
    calls = [{"name": c["name"], "args": c.get("args", {}), "id": f"c{i}"} for i, c in enumerate(step.get("calls", []))]
    return AIMessage(step.get("content", ""), tool_calls=calls)


def _text(message: BaseMessage) -> str:
    return message.content if isinstance(message.content, str) else json.dumps(message.content)


def _fences_hold(text: str, fence: Fence) -> list[str]:
    """What is wrong with the fences in a text: live markers alternate open and close and all carry the run's id."""
    wrong, open_ = [], False
    for closing, attrs in LIVE.findall(text):
        if f'id="{fence.id}"' not in attrs:
            wrong.append(f"a live marker without the run's id: {attrs!r}")
        if bool(closing) != open_:
            wrong.append("markers out of order")
        open_ = not closing
    if open_:
        wrong.append("a fence left open")
    return wrong


def _bindings(spec_yaml: str, hosts: list[str]) -> dict[str, Binding]:
    tools = parse(spec_yaml).tools
    return {
        name: Binding(
            name,
            "https://exciter.example/mcp",
            tuple(b.get("scopes", [])),
            b.get("access", "read"),
            tuple(b.get("allowTools", [])),
            tuple(hosts),
        )
        for name, b in tools.items()
    }


@pytest.mark.parametrize("path", CASES, ids=[p.stem for p in CASES])
async def test_a_steered_model_stays_within_what_rotor_guarantees(path: Path) -> None:
    case = YAML(typ="safe").load(path.read_text())
    fence = Fence()
    spec_yaml = case.get("workflow", DEFAULT_WORKFLOW)
    hosts = case.get("hosts", DEFAULT_HOSTS)
    answers = json.loads(json.dumps(case.get("answers", {})).replace(FENCE_ID, fence.id))
    tools = CorpusTools(_bindings(spec_yaml, hosts), answers, case.get("hints", DEFAULT_HINTS))
    model = RecordingModel(responses=[_message(step) for step in case["model"]])
    events: list[tuple[str, str, dict[str, Any]]] = []

    async def emit(kind: str, node: str, payload: dict[str, Any]) -> None:
        events.append((kind, node, payload))

    ctx = RunContext(
        spec=parse(spec_yaml), tools=tools, emit=emit, model=lambda: model, link_hosts=frozenset(hosts), fence=fence
    )
    outcome = await run_workflow(ctx, case.get("inputs", {}), thread_id=path.stem)
    expect = case.get("expect", {})
    assert outcome.status == expect.get("status", "succeeded"), outcome

    # 1. Every prompt starts with the fixed policy, and 2. all it quotes is fenced, unescapably.
    assert model.seen, "the model was never asked"
    for messages in model.seen:
        assert isinstance(messages[0], SystemMessage) and _text(messages[0]).startswith(SYSTEM_POLICY[:40])
        for m in messages:
            if isinstance(m, (HumanMessage, ToolMessage)):
                assert not _fences_hold(_text(m), fence), (_fences_hold(_text(m), fence), _text(m))
            if isinstance(m, ToolMessage):
                assert _text(m).startswith(f'<{UNTRUSTED_TAG} id="{fence.id}"'), _text(m)
        for planted in expect.get("fenced", []):
            human = next(m for m in messages if isinstance(m, HumanMessage))
            assert planted in _text(human) and f'<{UNTRUSTED_TAG} id="{fence.id}"' in _text(human)

    # 3. What a binding does not allow never reaches a tool.
    executed = [(b, name) for b, name, _ in tools.executed]
    for refused in expect.get("refused", []):
        assert all(name != refused for _, name in executed), (refused, executed)
    for done in expect.get("executed", []):
        assert any(name == done for _, name in executed), (done, executed)

    # 4. No host but the tools' appears in what was sent to a tool or in what an agent answered; a tool node's output
    # is what the tool said, which is data and stays as it is.
    limiter = LinkLimiter(hosts)
    sent = json.dumps([args for _, _, args in tools.executed])
    agents = {node for node, n in ctx.spec.nodes.items() if n["type"] == "agent"}
    output = json.dumps({node: value for node, value in outcome.output.items() if node in agents})
    assert limiter.limit(sent)[1] == [] and limiter.limit(output)[1] == [], (sent, output)
    for gone in expect.get("without", []):
        assert gone not in sent and gone not in output, gone
    for kept in expect.get("output_with", []):
        assert kept in output, (kept, output)
    kinds = [kind for kind, _, _ in events]
    for kind in expect.get("events", []):
        assert kind in kinds, (kind, kinds)
