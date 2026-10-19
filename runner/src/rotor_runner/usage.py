"""What each model call of a run used, told to the control plane as an llm.usage event: it totals usage per run,
person and day, across sandboxes, and stops a run past its limits (docs/api-contract-m7-usage.md)."""

from __future__ import annotations

from typing import Any
from uuid import UUID

from langchain_core.callbacks import AsyncCallbackHandler
from langchain_core.messages import BaseMessage
from langchain_core.outputs import LLMResult

from rotor_runner.events import EventSink, top_node


class UsageReporter(AsyncCallbackHandler):
    """Emits {model, inputTokens, outputTokens} for every model call, under the workflow node that made it."""

    def __init__(self, emit: EventSink) -> None:
        self.emit = emit
        self.nodes: dict[UUID, str] = {}

    async def on_chat_model_start(
        self,
        serialized: dict[str, Any],
        messages: list[list[BaseMessage]],
        *,
        run_id: UUID,
        metadata: dict[str, Any] | None = None,
        **_kwargs: Any,
    ) -> None:
        self.nodes[run_id] = top_node(metadata or {})

    async def on_llm_end(self, response: LLMResult, *, run_id: UUID, **_kwargs: Any) -> None:
        node = self.nodes.pop(run_id, "")
        inputs = outputs = 0
        model = str((response.llm_output or {}).get("model_name") or "")
        for generations in response.generations:
            for generation in generations:
                message = getattr(generation, "message", None)
                usage = getattr(message, "usage_metadata", None) or {}
                inputs += int(usage.get("input_tokens", 0))
                outputs += int(usage.get("output_tokens", 0))
                model = model or str((getattr(message, "response_metadata", None) or {}).get("model_name") or "")
        if inputs or outputs:
            await self.emit("llm.usage", node, {"model": model, "inputTokens": inputs, "outputTokens": outputs})
