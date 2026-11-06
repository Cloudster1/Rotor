"""A failed call says what failed: a token's refusal reaches the person as the control plane said it, and the address
a sandbox reaches a tool at stays in the log, never in what the run reports (#266)."""

from __future__ import annotations

import logging
from typing import Any

import pytest

from rotor_runner.mcp import Binding, McpGateway, TokenError, ToolFailedError, unaddressed

pytestmark = pytest.mark.anyio

# Nothing listens on the discard port, so a call there fails at once, as a tool that does not answer.
BACKCHANNEL = "http://127.0.0.1:9/t/exciter--run/api/v1/mcp"
REFUSAL = (
    "no token for exciter: exciter no longer knows Rotor. "
    "Ask an administrator to register it again under Admin, Tools, then connect your account again."
)


class Refusing:
    """The channel's tokens when the control plane could not mint one."""

    async def token(self, binding: str) -> str:
        raise TokenError(REFUSAL)


class Granting:
    async def token(self, binding: str) -> str:
        return "stub_oat_the-run-token"


async def emit(kind: str, node: str, payload: dict[str, Any]) -> None:
    return None


def gateway(tokens: Any) -> McpGateway:
    binding = Binding("exciter", BACKCHANNEL, ("mcp",), "read", ("whoami",))
    return McpGateway(bindings={"exciter": binding}, tokens=tokens, emit=emit)


async def test_a_token_refusal_passes_through_as_the_control_plane_said_it() -> None:
    with pytest.raises(TokenError) as refused:
        await gateway(Refusing()).call("exciter", "whoami", {}, "ask")
    assert str(refused.value) == REFUSAL
    assert "cannot be reached" not in str(refused.value)


async def test_an_agents_tools_meet_the_same_refusal() -> None:
    with pytest.raises(TokenError) as refused:
        await gateway(Refusing()).langchain_tools(["exciter"], "agent")
    assert str(refused.value) == REFUSAL


async def test_a_tool_that_does_not_answer_is_named_by_its_binding_and_its_address_is_only_logged(
    caplog: pytest.LogCaptureFixture,
) -> None:
    with caplog.at_level(logging.WARNING, logger="rotor_runner"), pytest.raises(ToolFailedError) as failed:
        await gateway(Granting()).call("exciter", "whoami", {}, "ask")
    assert not isinstance(failed.value, TokenError)
    assert str(failed.value).startswith("exciter did not list its tools")
    assert "127.0.0.1" not in str(failed.value)
    assert BACKCHANNEL in caplog.text


def test_an_address_in_an_errors_text_is_said_as_the_binding() -> None:
    text = f"Client error '401 Unauthorized' for url '{BACKCHANNEL}'"
    assert unaddressed(text, "exciter") == "Client error '401 Unauthorized' for url 'exciter'"
