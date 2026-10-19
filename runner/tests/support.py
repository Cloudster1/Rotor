"""What several test modules share."""

from __future__ import annotations

import json
from collections.abc import Sequence
from typing import Any

from langchain_core.language_models.fake_chat_models import FakeMessagesListChatModel, GenericFakeChatModel
from langchain_core.messages import BaseMessage
from langchain_core.outputs import ChatResult
from langchain_core.tools import BaseTool, StructuredTool
from pydantic import Field

from rotor_runner.mcp import Binding, ToolRefusedError, allowed


class ToolCallingModel(FakeMessagesListChatModel):
    """Answers with the scripted messages, tool calls included; binding tools changes nothing."""

    def bind_tools(self, tools: Sequence[Any], **_kwargs: Any) -> ToolCallingModel:
        return self


class StreamingModel(GenericFakeChatModel):
    """Streams its scripted answers word by word, as a provider does; binding tools changes nothing."""

    def bind_tools(self, tools: Sequence[Any], **_kwargs: Any) -> StreamingModel:
        return self


class RecordingModel(ToolCallingModel):
    """A scripted model that keeps every list of messages it was sent, to show what a prompt really held."""

    seen: list[list[BaseMessage]] = Field(default_factory=list)

    def _generate(self, messages: list[BaseMessage], *args: Any, **kwargs: Any) -> ChatResult:
        self.seen.append(list(messages))
        return super()._generate(messages, *args, **kwargs)


class CorpusTools:
    """Tools as a steered run meets them: answers from a table, offered through mcp.allowed as McpGateway does, and
    every call that reached a tool recorded."""

    def __init__(self, bindings: dict[str, Binding], answers: dict[str, Any], hints: dict[str, Any]) -> None:
        self.bindings, self.answers, self.hints = bindings, answers, hints
        self.executed: list[tuple[str, str, dict[str, Any]]] = []

    async def call(self, binding: str, name: str, arguments: dict[str, Any], node: str) -> Any:
        b = self.bindings.get(binding)
        if b is None or not allowed(b, name, self.hints.get(name)):
            raise ToolRefusedError(f"{binding} does not allow {name}")
        self.executed.append((binding, name, arguments))
        return self.answers.get(name, {})

    async def langchain_tools(self, bindings: list[str], node: str) -> list[BaseTool]:
        out: list[BaseTool] = []
        for name in bindings:
            b = self.bindings[name]
            prefix = f"{name}_" if len(bindings) > 1 else ""
            out += [self._tool(b, call, prefix) for call in b.allow_tools if allowed(b, call, self.hints.get(call))]
        return out

    def _tool(self, b: Binding, call: str, prefix: str) -> BaseTool:
        async def run(**arguments: Any) -> str:
            self.executed.append((b.name, call, arguments))
            return json.dumps(self.answers.get(call, {}))

        schema = {"type": "object", "properties": {}, "additionalProperties": True}
        return StructuredTool(name=prefix + call, description=f"{call} at {b.name}", args_schema=schema, coroutine=run)
