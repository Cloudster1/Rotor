"""Code nodes: run(state, ctx) in a child process with an empty environment, memory and CPU limits and a timeout.
Its tool calls come back here, where the node's bindings decide; only JSON goes in and out."""

from __future__ import annotations

import asyncio
import contextlib
import json
import os
import signal
import sys
from typing import TYPE_CHECKING, Any

from rotor_runner.config import (
    CODE_DEFAULT_MEMORY,
    CODE_DEFAULT_TIMEOUT_S,
    CODE_LINE_BYTES,
    CODE_LOG_BYTES,
    CODE_MAX_MEMORY,
    CODE_MAX_TIMEOUT_S,
    CODE_OPEN_FILES,
    CODE_OUTPUT_BYTES,
    DURATION_UNITS,
    MEBIBYTE,
)
from rotor_runner.mcp import ToolFailedError, ToolRefusedError

if TYPE_CHECKING:
    from asyncio.subprocess import Process

    from rotor_runner.compile import RunContext

# The child gets nothing of the runner's environment, so no token or key can be read there.
CHILD_ENV = {"PATH": "/usr/bin:/bin", "LANG": "C.UTF-8"}
MEMORY_UNITS = {"Mi": MEBIBYTE, "Gi": 1024 * MEBIBYTE}


def memory_limit(node: dict[str, Any]) -> int:
    text = str(node.get("memory") or "")
    size = int(text[:-2]) * MEMORY_UNITS[text[-2:]] if text else CODE_DEFAULT_MEMORY
    return min(size, CODE_MAX_MEMORY)


def timeout_of(node: dict[str, Any]) -> float:
    text = str(node.get("timeout") or "")
    seconds = float(text[:-1]) * DURATION_UNITS[text[-1]] if text else CODE_DEFAULT_TIMEOUT_S
    return min(seconds, CODE_MAX_TIMEOUT_S)


class CodeNodeRunner:
    """The CodeRunner a run's code nodes use."""

    async def run(self, node: dict[str, Any], state: dict[str, Any], ctx: RunContext) -> Any:
        from rotor_runner.limits import failed

        memory, seconds = memory_limit(node), timeout_of(node)
        # The child lowers its own limits before it reads the code: Python between fork and exec is unsafe in a
        # runner whose gRPC threads are running.
        limits = (str(memory), str(int(seconds) + 1), str(CODE_OPEN_FILES))
        child = await asyncio.create_subprocess_exec(
            sys.executable,
            "-I",
            "-B",
            "-m",
            "rotor_runner.codehost",
            *limits,
            stdin=asyncio.subprocess.PIPE,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.PIPE,
            env=CHILD_ENV,
            start_new_session=True,
            limit=CODE_LINE_BYTES,
        )
        try:
            async with asyncio.timeout(seconds):
                return await self._converse(child, node, state, ctx)
        except TimeoutError:
            raise failed("code_timeout", f"{node['id']} ran longer than {seconds:g} seconds") from None
        finally:
            _kill(child)
            await child.wait()

    async def _converse(self, child: Process, node: dict[str, Any], state: dict[str, Any], ctx: RunContext) -> Any:
        from rotor_runner.limits import failed

        assert child.stdin is not None and child.stdout is not None  # noqa: S101 - set by the PIPE arguments
        request = {
            "node": node["id"],
            "source": node["source"],
            "state": {"inputs": state.get("inputs", {}), "nodes": state.get("nodes", {})},
        }
        child.stdin.write((json.dumps(request) + "\n").encode())
        await child.stdin.drain()
        logged = 0
        while True:
            try:
                line = await child.stdout.readline()
            except ValueError:
                raise failed("code_output", f"{node['id']} sent more than {CODE_LINE_BYTES} bytes at once") from None
            if not line:
                code = await child.wait()
                said = (
                    (await child.stderr.read(CODE_LOG_BYTES)).decode(errors="replace").strip() if child.stderr else ""
                )
                last = said.splitlines()[-1] if said else ""
                if code in (-signal.SIGKILL, -signal.SIGXCPU) or "MemoryError" in last:
                    raise failed("code_limits", f"{node['id']} was stopped by its limits ({last or f'signal {-code}'})")
                raise failed(
                    "code_error", f"{node['id']} ended without a result (exit {code}{': ' + last if last else ''})"
                )
            message = json.loads(line)
            if "log" in message:
                text = str(message["log"].get("text", ""))
                if logged < CODE_LOG_BYTES:
                    cut = text[: CODE_LOG_BYTES - logged]
                    logged += len(cut)
                    suffix = "..." if logged >= CODE_LOG_BYTES else ""
                    await ctx.emit(
                        "log", node["id"], {"stream": message["log"].get("stream", "stdout"), "text": cut + suffix}
                    )
            elif "call" in message:
                await self._answer(child, node, message["call"], ctx)
            elif "error" in message:
                kind, text = message["error"].get("type", ""), message["error"].get("message", "")
                if kind == "MemoryError":
                    raise failed("code_memory", f"{node['id']} ran out of its {memory_limit(node) // MEBIBYTE} MiB")
                if kind == "NotJSON":
                    raise failed("code_output", f"{node['id']}: {text}")
                raise failed("code_error", f"{node['id']} raised {kind}: {text}")
            elif "result" in message:
                if len(line) > CODE_OUTPUT_BYTES:
                    raise failed("code_output", f"{node['id']} returned more than {CODE_OUTPUT_BYTES} bytes")
                return message["result"]

    async def _answer(self, child: Process, node: dict[str, Any], call: dict[str, Any], ctx: RunContext) -> None:
        assert child.stdin is not None  # noqa: S101 - set by the PIPE argument
        binding = str(call.get("binding", ""))
        reply: dict[str, Any] = {"id": call.get("id")}
        if binding not in node.get("tools", []):
            reply["error"] = f"{node['id']} may not use {binding}; list it in the node's tools"
        else:
            try:
                reply["value"] = await ctx.tools.call(
                    binding, str(call.get("name", "")), dict(call.get("arguments") or {}), node["id"]
                )
            except (ToolRefusedError, ToolFailedError) as err:
                reply["error"] = str(err)
        child.stdin.write((json.dumps({"reply": reply}) + "\n").encode())
        await child.stdin.drain()


def _kill(child: Process) -> None:
    """Ends the child and anything it started, which share its session."""
    if child.returncode is None:
        with contextlib.suppress(ProcessLookupError):
            os.killpg(child.pid, signal.SIGKILL)
