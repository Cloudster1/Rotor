"""The child process a code node runs in: it holds no token, and reaches the tools only by asking the runner.
JSON lines: the runner sends the code and state, the child sends tool calls, logs and the result."""

from __future__ import annotations

import json
import resource
import sys
import traceback
from typing import IO, Any


class ToolCallError(RuntimeError):
    """A tool call the runner refused, or the tool answered with an error."""


class Context:
    """What a code node's run(state, ctx) may do beyond its state: call the tools its node may use, and work in its
    step's workspace when it has one (#136)."""

    def __init__(self, protocol: IO[str], replies: IO[str], workspace: dict[str, Any] | None = None) -> None:
        self._protocol, self._replies, self._next = protocol, replies, 0
        self.workspace = Workspace(self) if workspace is not None else None

    def call_tool(self, binding: str, name: str, arguments: dict[str, Any] | None = None) -> Any:
        """Calls an MCP tool through the runner, which checks the binding allows it."""
        return self.ask("call", {"binding": binding, "name": name, "arguments": arguments or {}})

    def ask(self, kind: str, call: dict[str, Any]) -> Any:
        """Sends one numbered request to the runner and waits for its reply."""
        self._next += 1
        _send(self._protocol, {kind: {"id": self._next, **call}})
        reply = json.loads(self._replies.readline())["reply"]
        if reply.get("error"):
            raise ToolCallError(reply["error"])
        return reply.get("value")


class Workspace:
    """The step's work tree and toolchain, through the runner: the node holds neither the token nor the agent's
    secret, and each call is checked there as an agent's tool would be."""

    def __init__(self, ctx: Context) -> None:
        self._ctx = ctx

    def _call(self, name: str, **arguments: Any) -> Any:
        return self._ctx.ask("workspace", {"name": name, "arguments": arguments})

    def list_files(self, path: str = ".") -> list[str]:
        return list(self._call("list_files", path=path))

    def read_file(self, path: str, start_line: int = 1) -> str:
        return str(self._call("read_file", path=path, start_line=start_line))

    def search(self, text: str, path: str = ".") -> list[str]:
        return list(self._call("search", text=text, path=path))

    def run_command(self, argv: list[str], cwd: str = ".", timeout: float | None = None) -> dict[str, Any]:
        """Runs argv, never a shell line, in the toolchain; answers exitCode, timedOut, durationMs and output."""
        return dict(self._call("run_command", argv=argv, cwd=cwd, timeout=timeout))

    def write_file(self, path: str, content: str) -> int:
        return int(self._call("write_file", path=path, content=content))

    def edit_file(self, path: str, old: str, new: str, replace_all: bool = False) -> int:
        return int(self._call("edit_file", path=path, old=old, new=new, replace_all=replace_all))


class _Log:
    """Stands in for stdout and stderr: a node's prints become log lines, one per line, apart from the protocol."""

    def __init__(self, stream: str, protocol: IO[str]) -> None:
        self._stream, self._protocol, self._pending = stream, protocol, ""

    def write(self, text: str) -> int:
        self._pending += text
        while "\n" in self._pending:
            line, self._pending = self._pending.split("\n", 1)
            _send(self._protocol, {"log": {"stream": self._stream, "text": line}})
        return len(text)

    def flush(self) -> None:
        if self._pending:
            _send(self._protocol, {"log": {"stream": self._stream, "text": self._pending}})
            self._pending = ""


def _send(protocol: IO[str], message: dict[str, Any]) -> None:
    protocol.write(json.dumps(message) + "\n")
    protocol.flush()


def limit(memory: int, cpu_seconds: int, open_files: int) -> None:
    """Lowers this process's limits for good, before the node's code runs; it cannot raise them again."""
    resource.setrlimit(resource.RLIMIT_AS, (memory, memory))
    resource.setrlimit(resource.RLIMIT_CPU, (cpu_seconds, cpu_seconds))
    resource.setrlimit(resource.RLIMIT_NOFILE, (open_files, open_files))
    resource.setrlimit(resource.RLIMIT_FSIZE, (0, 0))


def main() -> None:
    memory, cpu_seconds, open_files = (int(arg) for arg in sys.argv[1:4])
    limit(memory, cpu_seconds, open_files)
    protocol = sys.stdout
    out, err = _Log("stdout", protocol), _Log("stderr", protocol)
    sys.stdout, sys.stderr = out, err
    request = json.loads(sys.stdin.readline())
    try:
        namespace: dict[str, Any] = {"__name__": "rotor_code_node"}
        # Running the author's code is what a code node is for; the process limits and the missing tokens contain it.
        exec(compile(request["source"], f"<node {request['node']}>", "exec"), namespace)  # noqa: S102
        run = namespace.get("run")
        if not callable(run):
            raise TypeError("the code defines no run(state, ctx) function")
        result = run(request["state"], Context(protocol, sys.stdin, request.get("workspace")))
        out.flush()
        err.flush()
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
