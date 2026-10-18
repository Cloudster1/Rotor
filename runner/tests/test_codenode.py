"""Code nodes run in a child without the run's secrets, within their limits, and reach tools only through the runner."""

from __future__ import annotations

from typing import Any

import pytest
from test_compile import FakeTools

from rotor_runner.codenode import CodeNodeRunner
from rotor_runner.compile import Outcome, RunContext, run_workflow
from rotor_runner.config import CODE_LOG_BYTES
from rotor_runner.spec import parse

pytestmark = pytest.mark.anyio


def workflow(source: str, extra: str = "", schema: str = "{type: object}") -> str:
    indented = "\n".join("      " + line for line in source.strip().splitlines())
    return f"""apiVersion: rotor.dev/v1
kind: Workflow
metadata: {{name: code}}
tools:
  t: {{tool: exciter, scopes: [mcp], allowTools: [whoami, forbidden]}}
  u: {{tool: exciter, scopes: [mcp], allowTools: [whoami]}}
limits: {{maxSteps: 5, timeout: 1m}}
entry: a
nodes:
  - id: a
    type: code
    tools: [t]
    output: {{schema: {schema}}}{extra}
    source: |
{indented}
"""


async def run_code(yaml: str, tools: FakeTools | None = None) -> tuple[Outcome, list[tuple[str, str, dict[str, Any]]]]:
    seen: list[tuple[str, str, dict[str, Any]]] = []

    async def emit(kind: str, node: str, payload: dict[str, Any]) -> None:
        seen.append((kind, node, payload))

    ctx = RunContext(spec=parse(yaml), tools=tools or FakeTools({}), emit=emit, code=CodeNodeRunner())
    return await run_workflow(ctx, {"n": 3}, thread_id="t"), seen


async def test_a_code_node_reads_its_state_and_returns_json() -> None:
    outcome, _ = await run_code(workflow('def run(state, ctx):\n    return {"double": state["inputs"]["n"] * 2}'))
    assert outcome.status == "succeeded", outcome
    assert outcome.output == {"a": {"double": 6}}


async def test_the_child_has_no_token_in_its_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("ROTOR_RUNNER_BOOTSTRAP", "bootstrap-secret")
    monkeypatch.setenv("ANTHROPIC_API_KEY", "key-secret")
    outcome, _ = await run_code(workflow("import os\ndef run(state, ctx):\n    return {'env': dict(os.environ)}"))
    assert outcome.status == "succeeded", outcome
    env = outcome.output["a"]["env"]
    assert "bootstrap-secret" not in str(env) and "key-secret" not in str(env)
    assert set(env) <= {"PATH", "LANG", "LC_CTYPE"}


async def test_a_node_past_its_timeout_is_stopped() -> None:
    outcome, _ = await run_code(workflow("def run(state, ctx):\n    while True:\n        pass", "\n    timeout: 1s"))
    assert (outcome.status, outcome.reason) == ("failed", "code_timeout")


async def test_a_node_past_its_memory_is_stopped() -> None:
    source = "def run(state, ctx):\n    hog = bytearray(512 * 1024 * 1024)\n    return {'n': len(hog)}"
    outcome, _ = await run_code(workflow(source, "\n    memory: 128Mi"))
    assert (outcome.status, outcome.reason) == ("failed", "code_memory")


async def test_a_result_that_is_not_json_or_breaks_the_schema_is_refused() -> None:
    outcome, _ = await run_code(workflow("def run(state, ctx):\n    return {'when': object()}"))
    assert (outcome.status, outcome.reason) == ("failed", "code_output")
    schema = "{type: object, required: [count], properties: {count: {type: integer}}}"
    outcome, _ = await run_code(workflow("def run(state, ctx):\n    return {'count': 'many'}", schema=schema))
    assert (outcome.status, outcome.reason) == ("failed", "output_schema")


async def test_an_error_in_the_code_names_its_line() -> None:
    outcome, _ = await run_code(workflow("def run(state, ctx):\n    x = 1\n    return {'r': 1 / 0}"))
    assert (outcome.status, outcome.reason) == ("failed", "code_error")
    assert "ZeroDivisionError" in outcome.output["message"] and "line 3" in outcome.output["message"]


async def test_tools_go_through_the_runner_and_the_nodes_bindings() -> None:
    source = """
def run(state, ctx):
    me = ctx.call_tool("t", "whoami")
    refusals = []
    for binding, name in (("u", "whoami"), ("t", "forbidden")):
        try:
            ctx.call_tool(binding, name)
        except Exception as err:
            refusals.append(str(err))
    return {"me": me, "refusals": refusals}
"""
    tools = FakeTools({"whoami": {"user": {"name": "bob"}}})
    outcome, _ = await run_code(workflow(source), tools)
    assert outcome.status == "succeeded", outcome
    assert outcome.output["a"]["me"] == {"user": {"name": "bob"}}
    first, second = outcome.output["a"]["refusals"]
    assert "may not use u" in first
    assert "does not allow forbidden" in second
    assert [c[1] for c in tools.calls] == ["whoami", "forbidden"]


async def test_prints_become_capped_log_events() -> None:
    source = f"def run(state, ctx):\n    print('hello')\n    print('x' * {CODE_LOG_BYTES * 2})\n    return {{}}"
    outcome, seen = await run_code(workflow(source))
    assert outcome.status == "succeeded", outcome
    logs = [p for kind, _, p in seen if kind == "log"]
    assert logs[0] == {"stream": "stdout", "text": "hello"}
    assert sum(len(p["text"]) for p in logs) <= CODE_LOG_BYTES + 3
    assert logs[-1]["text"].endswith("...")


async def test_a_node_cannot_write_files() -> None:
    source = (
        "def run(state, ctx):\n"
        "    with open('/tmp/rotor-code-node', 'w') as f:\n"
        "        f.write('x' * 10)\n"
        "    return {}"
    )
    outcome, _ = await run_code(workflow(source))
    assert (outcome.status, outcome.reason) == ("failed", "code_error")
