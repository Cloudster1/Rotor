"""Workspace steps (#136), whole on the runner's side: the branch fetched into a git directory of the runner's, calls
in the work tree and the toolchain, and a push only when a step that writes ended well."""

from __future__ import annotations

from collections.abc import AsyncIterator
from pathlib import Path
from typing import Any

import pytest
from langchain_core.messages import AIMessage, ToolMessage
from support import RecordingModel, ToolCallingModel
from workspace_support import BRANCH, SECRET, TOKEN, AgentStandIn, files_at, head_of, origin

from rotor_runner.codenode import CodeNodeRunner
from rotor_runner.compile import RunContext, run_workflow
from rotor_runner.config import MAX_COMMAND_OUTPUT, UNTRUSTED_TAG
from rotor_runner.mcp import Binding
from rotor_runner.repository import Repositories, Repository
from rotor_runner.spec import parse
from rotor_runner.workspace import WorkspaceConfig, Workspaces
from rotor_runner.workspace.agent import AgentClient
from rotor_runner.workspace.files import WorkspaceError

pytestmark = pytest.mark.anyio

IMAGE = "busybox-1.37"


class Tokens:
    async def token(self, binding: str) -> str:
        return TOKEN


class NoTools:
    async def call(self, binding: str, name: str, arguments: dict[str, Any], node: str) -> Any:
        raise AssertionError("no MCP tool is called")

    async def langchain_tools(self, bindings: list[str], node: str) -> list[Any]:
        return []


@pytest.fixture
async def place(tmp_path: Path) -> AsyncIterator[dict[str, Any]]:
    tree, git = tmp_path / "tree", tmp_path / "git"
    tree.mkdir()
    git.mkdir()
    agent = AgentStandIn(tree)
    yield {"origin": origin(tmp_path / "origin.git"), "agent": agent, "tree": tree, "git": git, "events": []}
    agent.close()


def workflow(node: str, access: str = "write", binding_access: str = "write") -> str:
    return f"""apiVersion: rotor.dev/v1
kind: Workflow
metadata: {{name: workspace}}
tools:
  repo:
    tool: armature
    scopes: [read]
    access: {binding_access}
    repository: {{project: CP, name: acme/portal}}
    allowTools: [list_repositories]
limits: {{maxSteps: 20, timeout: 1m}}
entry: work
nodes:
  - id: work
    workspace: {{repository: repo, branch: "{{{{ inputs.branch }}}}", image: {IMAGE}, access: {access}}}
{node}
"""


def context(place: dict[str, Any], spec_yaml: str, model: Any = None, binding_access: str = "write") -> RunContext:
    async def emit(kind: str, node: str, payload: dict[str, Any]) -> None:
        place["events"].append((kind, node, payload))

    repository = Repository("CP", "acme/portal", str(place["origin"]))
    bindings = {"repo": Binding("repo", "", ("read",), binding_access, ("list_repositories",), (), repository)}
    config = WorkspaceConfig({IMAGE: place["agent"].url}, SECRET, 60.0, place["tree"], place["git"])
    return RunContext(
        spec=parse(spec_yaml),
        tools=NoTools(),
        emit=emit,
        model=(lambda: model) if model is not None else None,
        code=CodeNodeRunner(),
        repositories=Repositories(bindings, Tokens()),
        workspaces=Workspaces(config, "run-1", emit),
    )


def call(name: str, i: int, **args: Any) -> dict[str, Any]:
    return {"name": name, "args": args, "id": f"c{i}"}


AGENT = "    type: agent\n    prompt: Add the notes and check them.\n"


async def test_a_writing_agent_commits_and_pushes_what_it_wrote_and_reads_files_fenced(place: dict[str, Any]) -> None:
    before = head_of(place["origin"])
    model = RecordingModel(
        responses=[
            # The calls of one answer run side by side, so what depends on another comes in a later answer.
            AIMessage(
                "",
                tool_calls=[
                    call("read_file", 0, path="README.md"),
                    call("write_file", 1, path="docs/notes.md", content="Passkeys first.\n"),
                    call("edit_file", 2, path="src/app.txt", old_text="hello", new_text="hello, passkey"),
                    call("run_command", 3, argv=["ln", "-s", "/proc/self/environ", "leak"]),
                    call("run_command", 4, argv=["mkdir", "-p", "build"]),
                ],
            ),
            AIMessage("", tool_calls=[call("run_command", 5, argv=["touch", "build/out.bin", "debug.log"])]),
            AIMessage("", tool_calls=[call("read_file", 6, path="leak"), call("list_files", 7)]),
            AIMessage("Done."),
        ]
    )
    ctx = context(place, workflow(AGENT), model)
    outcome = await run_workflow(ctx, {"branch": BRANCH}, thread_id="t1")
    assert outcome.status == "succeeded", outcome
    pushed = files_at(place["origin"])
    assert pushed["docs/notes.md"] == (0o100644, b"Passkeys first.\n")
    assert pushed["src/app.txt"][1] == b"hello, passkey\n"
    assert pushed["leak"] == (0o120000, b"/proc/self/environ"), "a link is committed as a link"
    assert not any(p.startswith("build/") or p.endswith(".log") for p in pushed), ".gitignore holds"
    assert head_of(place["origin"]) != before
    answers = {m.tool_call_id: str(m.content) for m in model.seen[-1] if isinstance(m, ToolMessage)}
    assert answers["c0"].startswith(f"<{UNTRUSTED_TAG} ") and "Sign in with a passkey" in answers["c0"]
    assert "symbolic link" in answers["c6"] and "ROTOR" not in answers["c6"]
    assert "leak@" in answers["c7"]
    kinds = [k for k, _, _ in place["events"]]
    assert kinds.count("workspace.command") == 3 and "workspace.checkout" in kinds and "workspace.pushed" in kinds
    assert place["agent"].resets == 1
    assert list(place["tree"].iterdir()) == [] and list(place["git"].iterdir()) == [], "nothing is kept"
    assert all(TOKEN not in str(payload) for _, _, payload in place["events"])


async def test_a_reading_step_offers_no_writes_and_pushes_nothing(place: dict[str, Any]) -> None:
    before = head_of(place["origin"])
    model = ToolCallingModel(
        responses=[
            AIMessage("", tool_calls=[call("run_command", 0, argv=["touch", "made-by-a-command.txt"])]),
            AIMessage("", tool_calls=[call("write_file", 1, path="x.txt", content="x")]),
            AIMessage("Read it."),
        ]
    )
    outcome = await run_workflow(
        context(place, workflow(AGENT, access="read"), model), {"branch": BRANCH}, thread_id="t"
    )
    assert outcome.status == "succeeded", outcome
    assert head_of(place["origin"]) == before
    assert not any(k == "workspace.pushed" for k, _, _ in place["events"])


async def test_a_step_that_fails_pushes_nothing(place: dict[str, Any]) -> None:
    before = head_of(place["origin"])
    code = """    type: code
    output: {schema: {type: object}}
    source: |
      def run(state, ctx):
          ctx.workspace.write_file("half.txt", "half done")
          raise RuntimeError("the build broke")
"""
    outcome = await run_workflow(context(place, workflow(code)), {"branch": BRANCH}, thread_id="t")
    assert outcome.status == "failed"
    assert head_of(place["origin"]) == before
    assert place["agent"].resets == 1 and list(place["tree"].iterdir()) == []


async def test_a_code_node_works_through_ctx_workspace_and_holds_no_token(place: dict[str, Any]) -> None:
    code = """    type: code
    output: {schema: {type: object}}
    source: |
      import os
      def run(state, ctx):
          ctx.workspace.write_file("made.txt", "by code")
          ran = ctx.workspace.run_command(["cat", "made.txt"])
          refused = ""
          try:
              ctx.workspace.run_command("cat made.txt")
          except Exception as err:
              refused = str(err)
          return {"ran": ran["output"], "exit": ran["exitCode"], "files": ctx.workspace.list_files("."),
                  "found": ctx.workspace.search("passkey"), "refused": refused, "env": dict(os.environ)}
"""
    outcome = await run_workflow(context(place, workflow(code)), {"branch": BRANCH}, thread_id="t")
    assert outcome.status == "succeeded", outcome
    out = outcome.output["work"]
    assert out["ran"] == "by code" and out["exit"] == 0 and "made.txt" in out["files"]
    assert out["found"] == ["README.md:3: Sign in with a passkey."]
    assert "list of arguments" in out["refused"]
    assert TOKEN not in str(out) and SECRET not in str(out)
    assert files_at(place["origin"])["made.txt"][1] == b"by code"


async def test_a_command_is_an_argv_with_a_timeout_and_its_output_capped(place: dict[str, Any]) -> None:
    agent = AgentClient(place["agent"].url, SECRET)
    for wrong in ("ls -la", [], [""], ["ls", "a\x00b"]):
        with pytest.raises(WorkspaceError):
            await agent.run(wrong, str(place["tree"]), 5)  # type: ignore[arg-type]
    with pytest.raises(WorkspaceError):
        await AgentClient(place["agent"].url, "a-guess").run(["true"], str(place["tree"]), 5)
    shell = await agent.run(["echo", "$HOME; rm -rf /"], str(place["tree"]), 5)
    assert shell.output == "$HOME; rm -rf /\n"
    slow = await agent.run(["sleep", "5"], str(place["tree"]), 1)
    assert slow.timed_out and "ran out of time" in slow.as_text()
    place["agent"].lie_about_output = True
    loud = await agent.run(["seq", "1", "100000"], str(place["tree"]), 5)
    assert loud.truncated and len(loud.output.encode()) <= MAX_COMMAND_OUTPUT + 64
    assert loud.output.startswith("1\n2\n") and loud.output.endswith("100000\n")


async def test_a_writing_step_needs_a_binding_that_may_push(place: dict[str, Any]) -> None:
    ctx = context(place, workflow(AGENT), ToolCallingModel(responses=[AIMessage("x")]), binding_access="read")
    outcome = await run_workflow(ctx, {"branch": BRANCH}, thread_id="t")
    assert outcome.status == "failed" and outcome.reason == "workspace_binding"


async def test_a_branch_that_is_not_there_fails_the_step_saying_so(place: dict[str, Any]) -> None:
    ctx = context(place, workflow(AGENT), ToolCallingModel(responses=[AIMessage("x")]))
    outcome = await run_workflow(ctx, {"branch": "rotor/none"}, thread_id="t")
    assert outcome.status == "failed" and outcome.reason == "workspace_error"
    assert "no branch rotor/none" in outcome.output["message"]
