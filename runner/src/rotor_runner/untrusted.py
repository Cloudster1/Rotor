"""An agent's tool calls as untrusted content (#40): links in what it sends a tool are limited to the tools' hosts,
and every answer comes back fenced as data, whichever gateway made the call."""

from __future__ import annotations

from collections.abc import Awaitable, Callable
from typing import Any

from langchain.agents.middleware import AgentMiddleware
from langchain.agents.middleware.types import ToolCallRequest
from langchain_core.messages import ToolMessage
from langgraph.types import Command

from rotor_runner.config import LINK_EVENT_HOSTS
from rotor_runner.events import EventSink
from rotor_runner.links import LinkLimiter
from rotor_runner.policy import Fence


async def report_removed(emit: EventSink, node: str, where: str, hosts: list[str], **extra: Any) -> None:
    """Says that links were cut, and to which hosts, so a person sees a run that was steered at one."""
    if hosts:
        await emit(
            "link.removed",
            node,
            {"where": where, "count": len(hosts), "hosts": sorted(set(hosts))[:LINK_EVENT_HOSTS], **extra},
        )


def message_text(content: Any) -> str:
    """A tool message's content as text, its text blocks joined."""
    if isinstance(content, str):
        return content
    parts: list[str] = []
    for block in content if isinstance(content, list) else [content]:
        if isinstance(block, str):
            parts.append(block)
        elif isinstance(block, dict) and isinstance(block.get("text"), str):
            parts.append(block["text"])
    return "".join(parts)


class UntrustedResults(AgentMiddleware):
    """Fences tool answers and limits the links in tool arguments for one agent node."""

    def __init__(self, fence: Fence, links: LinkLimiter, emit: EventSink, node: str) -> None:
        super().__init__()
        self.fence, self.links, self.emit, self.node = fence, links, emit, node

    async def awrap_tool_call(
        self,
        request: ToolCallRequest,
        handler: Callable[[ToolCallRequest], Awaitable[ToolMessage | Command[Any]]],
    ) -> ToolMessage | Command[Any]:
        call = request.tool_call
        args, removed = self.links.limit_value(call["args"])
        if removed:
            request = request.override(tool_call={**call, "args": args})
            await report_removed(self.emit, self.node, "arguments", removed, tool=call["name"])
        result = await handler(request)
        if isinstance(result, ToolMessage):
            fenced = self.fence.wrap(f"tool:{call['name']}", message_text(result.content))
            return result.model_copy(update={"content": fenced})
        return result
