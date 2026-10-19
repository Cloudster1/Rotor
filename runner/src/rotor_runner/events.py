"""What a run tells the control plane while it runs; types and payloads are in docs/api-contract-m2.md."""

from __future__ import annotations

import json
from collections.abc import Awaitable, Callable
from typing import Any

from rotor_runner.config import SUMMARY_CHARS

# EventSink records an event: its type, the node it belongs to, and a JSON payload.
type EventSink = Callable[[str, str, dict[str, Any]], Awaitable[None]]


def summary(value: Any, limit: int = SUMMARY_CHARS) -> str:
    """A value as short JSON text for an event, cut at limit characters."""
    try:
        text = value if isinstance(value, str) else json.dumps(value, default=str, ensure_ascii=False)
    except (TypeError, ValueError):
        text = str(value)
    return text if len(text) <= limit else text[: limit - 3] + "..."


def top_node(metadata: dict[str, Any]) -> str:
    """The workflow node a LangChain callback or stream chunk belongs to, past the agent's own inner nodes."""
    namespace = str(metadata.get("langgraph_checkpoint_ns") or metadata.get("checkpoint_ns") or "")
    top = namespace.split("|")[0].split(":")[0]
    return top or str(metadata.get("langgraph_node", ""))


async def discard(_type: str, _node: str, _payload: dict[str, Any]) -> None:
    """An EventSink for runs nobody watches, such as the self-check."""
