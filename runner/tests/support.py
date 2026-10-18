"""What several test modules share."""

from __future__ import annotations

from collections.abc import Sequence
from typing import Any

from langchain_core.language_models.fake_chat_models import FakeMessagesListChatModel


class ToolCallingModel(FakeMessagesListChatModel):
    """Answers with the scripted messages, tool calls included; binding tools changes nothing."""

    def bind_tools(self, tools: Sequence[Any], **_kwargs: Any) -> ToolCallingModel:
        return self
