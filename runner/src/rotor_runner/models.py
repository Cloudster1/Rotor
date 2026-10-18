"""The run's language model, built with init_chat_model from what StartRun names and the key a SecretGrant brings.
The key lives in this process's memory alone."""

from __future__ import annotations

from typing import Any

from langchain.chat_models import init_chat_model
from langchain_core.language_models import BaseChatModel

from rotor_runner.pb.rotor.runner.v1 import runner_pb2 as pb


def build_model(config: pb.ModelConfig, key: str) -> BaseChatModel:
    """A chat model of the provider's kind; usage is reported when streaming, so the run's token budget holds."""
    kwargs: dict[str, Any] = {"api_key": key}
    if config.kind == "anthropic":
        provider = "anthropic"
        if config.base_url:
            kwargs["base_url"] = config.base_url
    elif config.kind == "azure_openai":
        provider = "azure_openai"
        kwargs.update(
            azure_endpoint=config.base_url,
            azure_deployment=config.model,
            api_version=config.api_version,
            stream_usage=True,
        )
    elif config.kind in ("openai", "openai_compatible"):
        provider = "openai"
        kwargs["stream_usage"] = True
        if config.base_url:
            kwargs["base_url"] = config.base_url
    else:
        raise ValueError(f"a provider of kind {config.kind!r}, which this runner cannot build")
    model: BaseChatModel = init_chat_model(config.model, model_provider=provider, **kwargs)
    return model
