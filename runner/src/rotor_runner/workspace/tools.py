"""An agent's workspace tools (#136): reading, searching and running commands, and with write, writing and editing.
What they answer passes the agent's UntrustedResults like any tool's answer, so it reaches the model fenced as data."""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from typing import Any

from langchain_core.tools import BaseTool, StructuredTool, ToolException

from rotor_runner.workspace.files import WorkspaceError
from rotor_runner.workspace.step import Workspace

# The names an agent calls them by, as docs/api-contract-m13.md agrees.
READING = ("list_files", "read_file", "search", "run_command")
WRITING = ("write_file", "edit_file")


def _tool(name: str, description: str, call: Callable[..., Awaitable[str]]) -> BaseTool:
    async def guarded(**kwargs: Any) -> str:
        try:
            return await call(**kwargs)
        except WorkspaceError as err:
            raise ToolException(str(err)) from None

    return StructuredTool.from_function(
        coroutine=guarded, name=name, description=description, args_schema=_SCHEMAS[name], handle_tool_error=True
    )


def _object(properties: dict[str, Any], required: list[str]) -> dict[str, Any]:
    return {"type": "object", "properties": properties, "required": required, "additionalProperties": False}


_PATH = {"type": "string", "description": "A path inside the work tree, such as src or src/main.go."}
_SCHEMAS: dict[str, dict[str, Any]] = {
    "list_files": _object({"path": {**_PATH, "default": "."}}, []),
    "read_file": _object(
        {
            "path": _PATH,
            "start_line": {"type": "integer", "minimum": 1, "default": 1, "description": "The first line."},
        },
        ["path"],
    ),
    "search": _object(
        {"text": {"type": "string", "description": "Text to find, as written."}, "path": _PATH}, ["text"]
    ),
    "run_command": _object(
        {
            "argv": {
                "type": "array",
                "items": {"type": "string"},
                "minItems": 1,
                "description": "The program and its arguments, such as [go, test, ./...]; no shell runs it.",
            },
            "cwd": {**_PATH, "default": "."},
            "timeout_seconds": {
                "type": "integer",
                "minimum": 1,
                "description": "How long it may run; 15 minutes unless given.",
            },
        },
        ["argv"],
    ),
    "write_file": _object({"path": _PATH, "content": {"type": "string"}}, ["path", "content"]),
    "edit_file": _object(
        {
            "path": _PATH,
            "old_text": {"type": "string", "description": "The text to replace, exactly as the file holds it."},
            "new_text": {"type": "string"},
            "replace_all": {"type": "boolean", "default": False},
        },
        ["path", "old_text", "new_text"],
    ),
}


def agent_tools(ws: Workspace) -> list[BaseTool]:
    """The workspace tools of one step's agent; write_file and edit_file only when the step writes."""

    async def list_files(path: str = ".") -> str:
        return "\n".join(ws.list_files(path)) or "The directory is empty."

    async def read_file(path: str, start_line: int = 1) -> str:
        return ws.read_file(path, start_line)

    async def search(text: str, path: str = ".") -> str:
        return "\n".join(ws.search(text, path)) or f"Nothing under {path} holds that text."

    async def run_command(argv: list[str], cwd: str = ".", timeout_seconds: int | None = None) -> str:
        return (await ws.run_command(argv, cwd, timeout_seconds)).as_text()

    async def write_file(path: str, content: str) -> str:
        return f"Wrote {ws.write_file(path, content)} bytes to {path}."

    async def edit_file(path: str, old_text: str, new_text: str, replace_all: bool = False) -> str:
        return f"Replaced {ws.edit_file(path, old_text, new_text, replace_all)} occurrence(s) in {path}."

    tools = [
        _tool(
            "list_files", "List the files under a directory of the work tree, the run's branch checked out.", list_files
        ),
        _tool("read_file", "Read a text file of the work tree, from a line on.", read_file),
        _tool("search", "Find the lines holding a text in the work tree's files.", search),
        _tool(
            "run_command",
            "Run a program in the toolchain, in a directory of the work tree; give its argv.",
            run_command,
        ),
    ]
    if ws.writes:
        tools += [
            _tool(
                "write_file", "Write a file of the work tree, making its directories; the step pushes it.", write_file
            ),
            _tool("edit_file", "Replace a text in a file of the work tree exactly once, or every time.", edit_file),
        ]
    return tools
