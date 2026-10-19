"""The run's MCP tools: every call carries the binding's current token and stays within what the binding allows.
Tokens are asked for at each call and never enter the graph's state."""

from __future__ import annotations

import json
from collections.abc import Awaitable, Callable
from dataclasses import dataclass, field
from typing import Any, Protocol

from langchain_core.tools import BaseTool
from langchain_mcp_adapters.client import MultiServerMCPClient
from langchain_mcp_adapters.interceptors import MCPToolCallRequest, ToolCallInterceptor
from langchain_mcp_adapters.tools import load_mcp_tools

from rotor_runner.events import EventSink, summary
from rotor_runner.limits import ToolRate
from rotor_runner.spec import REVIEW_DECIDING
from rotor_runner.telemetry import RunTracing


@dataclass(frozen=True)
class Binding:
    """A tool binding of the spec, resolved by the control plane to a tool's MCP endpoint."""

    name: str
    mcp_url: str
    scopes: tuple[str, ...]
    access: str
    allow_tools: tuple[str, ...]
    # The tool's public hosts, where links in an agent's text may point (#40).
    link_hosts: tuple[str, ...] = ()

    @property
    def writes(self) -> bool:
        return self.access == "write"


class TokenProvider(Protocol):
    """Hands out the run's current access token for a binding; the runner's channel keeps it fresh."""

    async def token(self, binding: str) -> str: ...


class ToolRefusedError(PermissionError):
    """A call the binding does not allow."""


class ToolFailedError(RuntimeError):
    """A call the tool answered with an error."""


class ToolGateway(Protocol):
    """What nodes ask of the tools; McpGateway is the real one, tests bring their own."""

    async def call(self, binding: str, name: str, arguments: dict[str, Any], node: str) -> Any: ...

    async def langchain_tools(self, bindings: list[str], node: str) -> list[BaseTool]: ...


def allowed(binding: Binding, name: str, annotations: dict[str, Any] | None) -> bool:
    """Whether a binding offers a tool: listed in allowTools, never deciding a review, and only reading unless it
    may write."""
    if name not in binding.allow_tools or REVIEW_DECIDING.match(name):
        return False
    return binding.writes or bool((annotations or {}).get("readOnlyHint"))


def leaf(err: BaseException) -> BaseException:
    """The first error inside nested exception groups: what went wrong, beneath the client's plumbing."""
    while isinstance(err, BaseExceptionGroup) and err.exceptions:
        err = err.exceptions[0]
    return err


def result_value(result: Any) -> Any:
    """An MCP result as plain data: its structured content, else its text read as JSON when it is JSON."""
    structured = getattr(result, "structuredContent", None)
    if structured is not None:
        return structured
    texts = [getattr(block, "text", "") for block in getattr(result, "content", []) or []]
    text = "".join(texts)
    try:
        return json.loads(text)
    except ValueError:
        return {"text": text}


@dataclass
class McpGateway:
    """Calls the tools over streamable HTTP, one session per call, as langchain-mcp-adapters does."""

    bindings: dict[str, Binding]
    tokens: TokenProvider
    emit: EventSink
    # rate ends the run on a call past its toolCallsPerMinute; None calls without a limit, as tests do.
    rate: ToolRate | None = None
    tracing: RunTracing = field(default_factory=RunTracing)
    _annotations: dict[str, dict[str, dict[str, Any] | None]] = field(default_factory=dict)

    async def _connection(self, binding: Binding) -> dict[str, Any]:
        token = await self.tokens.token(binding.name)
        return {"transport": "streamable_http", "url": binding.mcp_url, "headers": {"Authorization": f"Bearer {token}"}}

    async def _client(self, binding: Binding) -> MultiServerMCPClient:
        return MultiServerMCPClient({binding.name: await self._connection(binding)})  # type: ignore[dict-item]

    async def annotations(self, binding: Binding) -> dict[str, dict[str, Any] | None]:
        """The tool's own hints for each of its MCP tools, listed once per run."""
        if binding.name not in self._annotations:
            async with (await self._client(binding)).session(binding.name) as session:
                listed = await session.list_tools()
            self._annotations[binding.name] = {
                t.name: t.annotations.model_dump() if t.annotations else None for t in listed.tools
            }
        return self._annotations[binding.name]

    async def call(self, binding: str, name: str, arguments: dict[str, Any], node: str) -> Any:
        b = self.bindings.get(binding)
        if b is None:
            raise ToolRefusedError(f"{binding} is not one of the workflow's tools")
        try:
            hints = await self.annotations(b)
        except Exception as err:
            raise ToolFailedError(f"{binding} cannot be reached at {b.mcp_url}: {leaf(err)}") from err
        if not allowed(b, name, hints.get(name)):
            raise ToolRefusedError(f"{binding} does not allow {name}")
        if self.rate is not None:
            self.rate.check()
        try:
            with self.tracing.maybe(f"mcp {name}", {"rotor.binding": binding, "mcp.tool": name, "rotor.node": node}):
                async with (await self._client(b)).session(binding) as session:
                    result = await session.call_tool(name, arguments)
        except Exception as err:
            raise ToolFailedError(f"calling {name} at {binding} failed: {leaf(err)}") from err
        failed = bool(getattr(result, "isError", False))
        value = result_value(result)
        await self.emit(
            "tool.called",
            node,
            {
                "binding": binding,
                "tool": name,
                "arguments": arguments,
                "resultSummary": summary(value),
                "ok": not failed,
            },
        )
        if failed:
            raise ToolFailedError(f"{name} at {binding} answered an error: {summary(value)}")
        return value

    async def langchain_tools(self, bindings: list[str], node: str) -> list[BaseTool]:
        out: list[BaseTool] = []
        for name in bindings:
            b = self.bindings[name]
            listed = await load_mcp_tools(
                None,
                connection=await self._connection(b),  # type: ignore[arg-type]
                server_name=name,
                tool_interceptors=[self._interceptor(b, node)],
                tool_name_prefix=len(bindings) > 1,
            )
            prefix = f"{name}_" if len(bindings) > 1 else ""
            out += [t for t in listed if allowed(b, t.name.removeprefix(prefix), t.metadata)]
        return out

    def _interceptor(self, binding: Binding, node: str) -> ToolCallInterceptor:
        async def intercept(
            request: MCPToolCallRequest, handler: Callable[[MCPToolCallRequest], Awaitable[Any]]
        ) -> Any:
            if self.rate is not None:
                self.rate.check()
            token = await self.tokens.token(binding.name)
            span = {"rotor.binding": binding.name, "mcp.tool": request.name, "rotor.node": node}
            with self.tracing.maybe(f"mcp {request.name}", span):
                result = await handler(request.override(headers={"Authorization": f"Bearer {token}"}))
            await self.emit(
                "tool.called",
                node,
                {
                    "binding": binding.name,
                    "tool": request.name,
                    "arguments": request.args,
                    "resultSummary": summary(result_value(result)),
                    "ok": not bool(getattr(result, "isError", False)),
                },
            )
            return result

        return intercept
