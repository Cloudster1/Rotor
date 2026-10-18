"""A run's limits that LangGraph does not enforce itself: the model's token budget."""

from __future__ import annotations

from typing import Any

from langchain_core.callbacks import AsyncCallbackHandler
from langchain_core.outputs import LLMResult


class TokenBudgetError(RuntimeError):
    """The run's model read and wrote more tokens than its limits allow."""


class TokenBudget(AsyncCallbackHandler):
    """Counts the tokens every model call of the run used and stops the run past its maxTokens."""

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
