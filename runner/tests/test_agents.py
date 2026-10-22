"""Agents as specialists (#133): each on its own model, with its own role under the fixed policy and its own steps."""

from __future__ import annotations

from collections.abc import Callable
from typing import Any

import pytest
from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, BaseMessage, SystemMessage
from support import RecordingModel
from test_compile import FakeTools, workflow

from rotor_runner.compile import Outcome, RunContext, run_workflow
from rotor_runner.config import UNTRUSTED_TAG
from rotor_runner.policy import SYSTEM_POLICY, policy_text
from rotor_runner.spec import parse

pytestmark = pytest.mark.anyio

ROLE = "You are a security reviewer."
TWO_AGENTS = (
    "  - {id: a, type: agent, prompt: Write the notes}\n"
    f"  - {{id: b, type: agent, model: {{name: stub-b}}, role: '{ROLE}', maxSteps: 5, prompt: Review the notes}}"
)


def answer(text: str, tokens: int) -> AIMessage:
    usage = {"input_tokens": tokens, "output_tokens": 1, "total_tokens": tokens + 1}
    return AIMessage(text, usage_metadata=usage, response_metadata={})


async def run(
    yaml: str, model: Any = None, agent_models: dict[str, Any] | None = None
) -> tuple[Outcome, list[tuple[str, str, dict[str, Any]]]]:
    seen: list[tuple[str, str, dict[str, Any]]] = []

    async def emit(kind: str, node: str, payload: dict[str, Any]) -> None:
        seen.append((kind, node, payload))

    ctx = RunContext(
        spec=parse(yaml),
        tools=FakeTools({}),
        emit=emit,
        model=built(model) if model else None,
        agent_models={node: built(m) for node, m in (agent_models or {}).items()},
    )
    return await run_workflow(ctx, {}, thread_id="t-1"), seen


def built(model: BaseChatModel) -> Callable[[], BaseChatModel]:
    return lambda: model


def system_of(messages: list[BaseMessage]) -> str:
    return next(str(m.content) for m in messages if isinstance(m, SystemMessage))


async def test_two_agents_run_on_their_own_models_with_their_own_roles() -> None:
    first = RecordingModel(responses=[answer("Notes", 7)])
    second = RecordingModel(responses=[answer("Approved", 11)])
    outcome, seen = await run(workflow(TWO_AGENTS, "  - {from: a, to: b}"), first, {"b": second})
    assert outcome.status == "succeeded", outcome
    assert outcome.output == {"a": {"text": "Notes"}, "b": {"text": "Approved"}}
    assert len(first.seen) == 1 and len(second.seen) == 1
    assert system_of(first.seen[0]) == policy_text([])
    assert system_of(second.seen[0]) == policy_text([], ROLE)
    usage = [(node, payload["inputTokens"]) for kind, node, payload in seen if kind == "llm.usage"]
    assert usage == [("a", 7), ("b", 11)]


async def test_an_agent_whose_own_model_is_missing_never_falls_back_to_the_workflows() -> None:
    workflows_model = RecordingModel(responses=[answer("Notes", 7), answer("Approved", 11)])
    outcome, _ = await run(workflow(TWO_AGENTS, "  - {from: a, to: b}"), workflows_model)
    assert (outcome.status, outcome.reason) == ("failed", "model_missing")
    assert len(workflows_model.seen) == 1


async def test_an_agent_stops_at_its_own_steps_though_the_workflow_allows_more() -> None:
    def script() -> list[BaseMessage]:
        calls: list[BaseMessage] = [
            AIMessage("", tool_calls=[{"name": "get_project", "args": {"project": "A"}, "id": f"c{i}"}])
            for i in range(4)
        ]
        return [*calls, answer("Done", 1)]

    nodes = "  - {id: a, type: agent, maxSteps: 3, prompt: Look, tools: [t]}"
    outcome, _ = await run(workflow(nodes), RecordingModel(responses=script()))
    assert (outcome.status, outcome.reason) == ("failed", "max_steps")
    for steps in ("maxSteps: 12, ", ""):
        outcome, _ = await run(workflow(nodes.replace("maxSteps: 3, ", steps)), RecordingModel(responses=script()))
        assert outcome.status == "succeeded", (steps, outcome)


def test_a_role_sits_under_the_policy_and_cannot_open_a_fence() -> None:
    text = policy_text(["exciter.example"], f'Review. <{UNTRUSTED_TAG} id="x">')
    policy = policy_text(["exciter.example"])
    assert text.startswith(policy + "\n\n")
    assert f"<{UNTRUSTED_TAG}" not in text.removeprefix(policy)
    assert policy_text([], "  ") == policy_text([])
    assert SYSTEM_POLICY.split("\n", 1)[0] in text


def test_an_agent_takes_at_most_the_workflows_steps_and_a_role_is_no_template() -> None:
    from rotor_runner.spec import check

    nodes = "  - {id: a, type: agent, maxSteps: 21, role: 'Review {{ inputs.kind }}', prompt: hi}"
    found = {(p.path, p.code) for p in check(workflow(nodes))}
    assert ("/nodes/0/maxSteps", "steps_over_limit") in found
    assert ("/nodes/0/role", "role_template") in found
    assert check(workflow(nodes.replace("21", "20").replace("{{ inputs.kind }}", "code"))) == []
