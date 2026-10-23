"""The test node (#138): the tests run as a step and each case of the tool's JUnit is counted and named. A failing
test is a result; a command that cannot run, runs out of time or reports nothing fails the step, saying why."""

from __future__ import annotations

from collections.abc import Callable
from pathlib import Path
from typing import TYPE_CHECKING, Any, cast

from rotor_runner import junit
from rotor_runner.config import (
    DURATION_UNITS,
    MAX_TEST_CASES,
    MAX_TEST_FAILURES,
    MAX_TEST_REPORT,
    TEST_DEFAULT_TIMEOUT_S,
    TEST_EVENT_FAILURES,
    TEST_LOG_TAIL_CHARS,
    TEST_REASON_TAIL_CHARS,
)
from rotor_runner.limits import failed
from rotor_runner.workspace import CommandResult, Workspace, WorkspaceError, WorkTree

if TYPE_CHECKING:
    from rotor_runner.compile import RunContext, State

# The reasons a test step fails with.
COMMAND, TIMEOUT, REPORT = "test_command", "test_timeout", "test_report"


def tests_step(node: dict[str, Any], ctx: RunContext) -> Callable[..., Any]:
    """The node's step: the tests run in a workspace that only reads the branch, whatever the spec says."""

    async def run(state: State) -> dict[str, Any]:
        if ctx.workspaces is None:
            raise failed("workspace_unavailable", f"{node['id']} needs a workspace, and this runner has none")
        reading = {**node, "workspace": {**node["workspace"], "access": "read"}}
        async with ctx.workspaces.step(reading, cast(dict[str, Any], state), ctx) as ws:
            value = await run_tests(node, ws)
        await ctx.emit("test.reported", node["id"], reported(value))
        return {"nodes": {node["id"]: value}}

    return run


async def run_tests(node: dict[str, Any], ws: Workspace) -> dict[str, Any]:
    """Runs the node's command in the work tree and reads its reports into the node's output."""
    node_id = node["id"]
    timeout = _seconds(str(node.get("timeout") or "")) or TEST_DEFAULT_TIMEOUT_S
    try:
        result = await ws.run_command(list(node["command"]), ".", timeout)
    except WorkspaceError as err:
        raise failed(COMMAND, f"the command of {node_id} could not run: {err}") from None
    if result.timed_out:
        raise failed(TIMEOUT, f"the tests of {node_id} ran out of time after {result.duration_ms / 1000:.0f} s")
    cases: list[junit.Case] = []
    for path, data in read_reports(ws.tree, list(node.get("reports") or []), node_id, result):
        try:
            cases.extend(junit.parse(data))
        except junit.ReportError as err:
            raise failed(REPORT, f"{node_id} could not read its report {path}: {err}") from None
    value = summarize(cases, result.output, ws.tree.root)
    if result.exit_code != 0 and value["failed"] == 0:
        raise failed(
            COMMAND,
            f"the command of {node_id} exited with code {result.exit_code}, and its reports name no failing test;"
            f" its output ends: {_tail(result)}",
        )
    return value


def read_reports(tree: WorkTree, paths: list[str], node_id: str, result: CommandResult) -> list[tuple[str, bytes]]:
    """Each report as the test tool wrote it, never through a link, all together at most MAX_TEST_REPORT."""
    if not paths:
        raise failed(REPORT, f"{node_id} names no report to read")
    out: list[tuple[str, bytes]] = []
    left = MAX_TEST_REPORT
    for path in paths:
        try:
            data, more = tree.read_bytes(path, left)
        except WorkspaceError as err:
            raise failed(
                REPORT,
                f"{node_id} could not read its report {path} after its command exited with code"
                f" {result.exit_code}: {err} Its output ends: {_tail(result)}",
            ) from None
        if more:
            raise failed(REPORT, f"the reports of {node_id} are larger than {MAX_TEST_REPORT} bytes together")
        left -= len(data)
        out.append((path, data))
    return out


def summarize(cases: list[junit.Case], output: str, root: Path) -> dict[str, Any]:
    """The node's output: counts of every case, the failures and cases it names, and the end of the output."""
    count = {r: sum(1 for c in cases if c.result == r) for r in (junit.PASSED, junit.FAILED, junit.SKIPPED)}
    failing = [c for c in cases if c.result == junit.FAILED][:MAX_TEST_FAILURES]
    # Past the cap, the cases that verify a test case or fail are kept before the rest.
    kept = sorted(range(len(cases)), key=lambda i: (not (cases[i].verifies or cases[i].result == junit.FAILED), i))
    named = sorted(kept[:MAX_TEST_CASES])
    return {
        **count,
        "failures": [{"name": c.name, "message": c.message, "file": _relative(c.file, root)} for c in failing],
        "cases": [
            {"name": cases[i].name, "result": cases[i].result, "verifies": list(cases[i].verifies)} for i in named
        ],
        "logTail": output[-TEST_LOG_TAIL_CHARS:],
    }


def reported(value: dict[str, Any]) -> dict[str, Any]:
    """What the test.reported event says: the counts and the first failures, for the run's page."""
    failures = [{"name": f["name"], "file": f["file"]} for f in value["failures"][:TEST_EVENT_FAILURES]]
    return {**{k: value[k] for k in (junit.PASSED, junit.FAILED, junit.SKIPPED)}, "failures": failures}


def _relative(file: str, root: Path) -> str:
    """A report's file as the repository names it; a tool that writes the absolute path names the work tree too."""
    prefix = f"{root}/"
    return file[len(prefix) :] if file.startswith(prefix) else file


def _tail(result: CommandResult) -> str:
    return result.output[-TEST_REASON_TAIL_CHARS:].strip() or "(nothing)"


def _seconds(duration: str) -> float:
    return float(duration[:-1]) * DURATION_UNITS[duration[-1]] if duration else 0.0
