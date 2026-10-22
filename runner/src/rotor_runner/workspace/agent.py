"""The runner's side of the exec agent in a toolchain container (#136): an argv, never a shell line, sent over the
pod's loopback with the sandbox's secret, which code nodes do not hold."""

from __future__ import annotations

import asyncio
import time
from dataclasses import dataclass
from typing import Any

import httpx

from rotor_runner.config import (
    AGENT_POLL_S,
    AGENT_PROBE_S,
    AGENT_READY_S,
    AGENT_SLACK_S,
    MAX_ARGV_BYTES,
    MAX_COMMAND_OUTPUT,
    OUTPUT_NOTE_BYTES,
)
from rotor_runner.workspace.files import WorkspaceError


@dataclass(frozen=True)
class CommandResult:
    """How a command ended, and what it wrote: its first and last half when there was more than the cap."""

    exit_code: int
    timed_out: bool
    duration_ms: int
    output: str
    truncated: bool

    def as_text(self) -> str:
        """The result as an agent reads it: how it ended, then the output."""
        if self.timed_out:
            head = f"The command ran out of time after {self.duration_ms / 1000:.1f} s and was ended."
        else:
            head = f"The command exited with code {self.exit_code} after {self.duration_ms / 1000:.1f} s."
        return f"{head}\n{self.output}"


def check_argv(argv: Any) -> list[str]:
    """An argument list the agent may run: strings, the first naming a program, with no NUL byte in any."""
    if not isinstance(argv, list) or not argv or not all(isinstance(a, str) for a in argv):
        raise WorkspaceError("Give the command as a list of arguments, such as ['go', 'test', './...'], not as a line.")
    if not argv[0] or any("\x00" in a for a in argv):
        raise WorkspaceError("Name the program first, and put no NUL byte in an argument.")
    if sum(len(a.encode()) for a in argv) > MAX_ARGV_BYTES:
        raise WorkspaceError(f"The command's arguments are longer than {MAX_ARGV_BYTES} bytes; write them to a file.")
    return argv


class AgentClient:
    """One toolchain container's agent, at a loopback port of the sandbox."""

    def __init__(self, url: str, secret: str) -> None:
        self.url = url.rstrip("/")
        self._headers = {"Authorization": f"Bearer {secret}"}

    async def ready(self, within: float = AGENT_READY_S) -> None:
        """Waits for the agent to listen, as its container may start a moment after the runner's."""
        deadline = time.monotonic() + within
        async with httpx.AsyncClient(timeout=AGENT_PROBE_S) as client:
            while True:
                try:
                    if (await client.get(f"{self.url}/healthz")).status_code < httpx.codes.BAD_REQUEST:
                        return
                except httpx.HTTPError:
                    pass
                if time.monotonic() > deadline:
                    raise WorkspaceError(
                        f"The toolchain's agent did not answer within {within:g} s; the sandbox lacks it."
                    )
                await asyncio.sleep(AGENT_POLL_S)

    async def run(
        self, argv: list[str], cwd: str, timeout_s: float, env: dict[str, str] | None = None
    ) -> CommandResult:
        body = {
            "argv": check_argv(argv),
            "dir": cwd,
            "env": [f"{k}={v}" for k, v in (env or {}).items()],
            "timeoutSeconds": max(1, int(timeout_s)),
            "maxOutputBytes": MAX_COMMAND_OUTPUT,
        }
        answer = await self._post("/exec", body, timeout_s + AGENT_SLACK_S)
        if answer.get("error"):
            raise WorkspaceError(str(answer["error"]))
        output = str(answer.get("output", ""))
        truncated = bool(answer.get("truncated"))
        # The agent caps the output; the runner holds it to the cap too, whatever answers on the loopback.
        if len(output.encode()) > MAX_COMMAND_OUTPUT + OUTPUT_NOTE_BYTES:
            half = MAX_COMMAND_OUTPUT // 2
            output, truncated = output[:half] + "\n[... output left out ...]\n" + output[-half:], True
        return CommandResult(
            exit_code=int(answer.get("exitCode", -1)),
            timed_out=bool(answer.get("timedOut")),
            duration_ms=int(answer.get("durationMs", 0)),
            output=output,
            truncated=truncated,
        )

    async def reset(self) -> dict[str, Any]:
        """Ends every process the steps started in the container and empties its caches."""
        return await self._post("/reset", {}, AGENT_SLACK_S)

    async def _post(self, path: str, body: dict[str, Any], timeout: float) -> dict[str, Any]:
        try:
            async with httpx.AsyncClient(timeout=timeout) as client:
                response = await client.post(f"{self.url}{path}", json=body, headers=self._headers)
        except httpx.HTTPError as err:
            raise WorkspaceError(f"The toolchain's agent did not answer: {type(err).__name__}.") from err
        if response.status_code != httpx.codes.OK:
            raise WorkspaceError(response.text.strip() or f"The toolchain's agent answered {response.status_code}.")
        answer: dict[str, Any] = response.json()
        return answer
