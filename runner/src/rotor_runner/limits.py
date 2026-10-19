"""A run's limits that LangGraph does not enforce itself: the model's token budget and the rate of tool calls.
Totals across sandboxes, cost and quotas are the control plane's, which sees every sandbox of a run."""

from __future__ import annotations

import time
from collections import deque
from collections.abc import Callable
from typing import Any

from langchain_core.callbacks import AsyncCallbackHandler
from langchain_core.outputs import LLMResult

from rotor_runner.config import TOOL_RATE_WINDOW_S


class RunEnded(Exception):  # noqa: N818 - it is how a run ends, not always an error
    """A run that stops before its graph does, with the outcome and reason the control plane records."""

    def __init__(self, status: str, reason: str, message: str) -> None:
        super().__init__(message)
        self.status, self.reason = status, reason


def failed(reason: str, message: str) -> RunEnded:
    return RunEnded("failed", reason, message)


class ToolRateExceeded(RunEnded):
    """A run that called its tools more often in a minute than its limits allow."""

    def __init__(self, limit: int) -> None:
        super().__init__("failed", "tool_rate", f"the run called its tools more than {limit} times in a minute")


class ToolRate:
    """Counts a run's tool calls in a sliding minute and ends the run on the one past its limit."""

    def __init__(self, per_minute: int, clock: Callable[[], float] = time.monotonic) -> None:
        self.limit, self.clock = per_minute, clock
        self.calls: deque[float] = deque()

    def check(self) -> None:
        now = self.clock()
        while self.calls and now - self.calls[0] >= TOOL_RATE_WINDOW_S:
            self.calls.popleft()
        if len(self.calls) >= self.limit:
            raise ToolRateExceeded(self.limit)
        self.calls.append(now)


class TokenBudgetError(RuntimeError):
    """The run's model read and wrote more tokens than its limits allow."""


class TokenBudget(AsyncCallbackHandler):
    """Counts the tokens every model call in this sandbox used and stops the run past its maxTokens; the control
    plane counts across sandboxes."""

    raise_error = True

    def __init__(self, limit: int) -> None:
        self.limit, self.used = limit, 0

    async def on_llm_end(self, response: LLMResult, **_kwargs: Any) -> None:
        for generations in response.generations:
            for generation in generations:
                usage = getattr(getattr(generation, "message", None), "usage_metadata", None) or {}
                self.used += int(usage.get("total_tokens", 0))
        if self.used > self.limit:
            raise TokenBudgetError(f"the run used {self.used} tokens, over its limit of {self.limit}")
