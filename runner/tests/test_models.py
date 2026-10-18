"""Each provider kind becomes the chat model LangChain builds for it, with the key and usage reporting."""

import pytest

from rotor_runner.models import build_model
from rotor_runner.pb.rotor.runner.v1 import runner_pb2 as pb


def test_each_kind_builds_its_model() -> None:
    anthropic = build_model(pb.ModelConfig(kind="anthropic", model="claude-sonnet-5-5"), "key")
    assert type(anthropic).__name__ == "ChatAnthropic"
    compatible = build_model(
        pb.ModelConfig(kind="openai_compatible", model="llama", base_url="http://ollama:11434/v1"), "key"
    )
    assert type(compatible).__name__ == "ChatOpenAI"
    assert str(compatible.openai_api_base) == "http://ollama:11434/v1"  # type: ignore[attr-defined]
    assert compatible.stream_usage is True  # type: ignore[attr-defined]
    azure = build_model(
        pb.ModelConfig(
            kind="azure_openai", model="gpt", base_url="https://x.openai.azure.com", api_version="2024-10-21"
        ),
        "key",
    )
    assert type(azure).__name__ == "AzureChatOpenAI"


def test_an_unknown_kind_is_refused() -> None:
    with pytest.raises(ValueError, match="cannot build"):
        build_model(pb.ModelConfig(kind="mystery", model="m"), "key")
