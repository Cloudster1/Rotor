"""The child process a code node runs in: it holds no token, and reaches the tools only by asking the runner.
JSON lines: the runner sends the code and state, the child sends tool calls, logs and the result."""

from __future__ import annotations

import json
import sys
import traceback
from typing import IO, Any


class ToolCallError(RuntimeError):
    """A tool call the runner refused, or the tool answered with an error."""


class Context:
    """What a code node's run(state, ctx) may do beyond its state: call the tools its node may use."""

    def __init__(self, protocol: IO[str], replies: IO[str]) -> None:
        self._protocol, self._replies, self._next = protocol, replies, 0

    def call_tool(self, binding: str, name: str, arguments: dict[str, Any] | None = None) -> Any:
        """Calls an MCP tool through the runner, which checks the binding allows it."""
        self._next += 1
        _send(
            self._protocol, {"call": {"id": self._next, "binding": binding, "name": name, "arguments": arguments or {}}}
        )
        reply = json.loads(self._replies.readline())["reply"]
        if reply.get("error"):
            raise ToolCallError(reply["error"])
        return reply.get("value")


class _Log:
    """Stands in for stdout and stderr, so a node's prints become log lines and never break the protocol."""

    def __init__(self, stream: str, protocol: IO[str]) -> None:
        self._stream, self._protocol = stream, protocol

    def write(self, text: str) -> int:
        if text:
            _send(self._protocol, {"log": {"stream": self._stream, "text": text}})
        return len(text)

    def flush(self) -> None:
        self._protocol.flush()


def _send(protocol: IO[str], message: dict[str, Any]) -> None:
    protocol.write(json.dumps(message) + "\n")
    protocol.flush()


def main() -> None:
    protocol = sys.stdout
    sys.stdout = _Log("stdout", protocol)
    sys.stderr = _Log("stderr", protocol)
    request = json.loads(sys.stdin.readline())
    try:
        namespace: dict[str, Any] = {"__name__": "rotor_code_node"}
        # Running the author's code is what a code node is for; the process limits and the missing tokens contain it.
        exec(compile(request["source"], f"<node {request['node']}>", "exec"), namespace)  # noqa: S102
        run = namespace.get("run")
        if not callable(run):
            raise TypeError("the code defines no run(state, ctx) function")
        result = run(request["state"], Context(protocol, sys.stdin))
        try:
            encoded = json.dumps({"result": result})
        except (TypeError, ValueError) as err:
            _send(
                protocol, {"error": {"type": "NotJSON", "message": f"run returned something that is not JSON: {err}"}}
            )
            return
        protocol.write(encoded + "\n")
        protocol.flush()
    except MemoryError:
        _send(protocol, {"error": {"type": "MemoryError", "message": "the code ran out of memory"}})
    except BaseException as err:
        last = traceback.extract_tb(err.__traceback__)[-1:] if err.__traceback__ else []
        where = f" (line {last[0].lineno})" if last and last[0].filename.startswith("<node") else ""
        _send(protocol, {"error": {"type": type(err).__name__, "message": f"{err}{where}"}})


if __name__ == "__main__":
    main()
