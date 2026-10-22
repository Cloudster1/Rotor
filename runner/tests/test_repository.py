"""A repository binding reaches the runner as a clone address, and its token as any binding's: in memory, cut from
all the runner sends, and only for a binding that names a repository (#137)."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import anyio
import pytest
from test_compile import FakeTools

from rotor_runner.compile import RunContext
from rotor_runner.control import Channel, bindings_from
from rotor_runner.mcp import ToolRefusedError
from rotor_runner.pb.rotor.runner.v1 import runner_pb2 as pb
from rotor_runner.redact import REDACTED
from rotor_runner.repository import Repositories
from rotor_runner.spec import parse

pytestmark = pytest.mark.anyio

FIXTURE = Path(__file__).resolve().parents[2] / "api" / "workflow-fixtures" / "valid" / "branch-for-an-issue.yaml"
CLONE_URL = "http://tool-stub:8080/t/armature--git/api/v1/git/CP/acme/portal.git"
TOKEN = "stub_oat_the-binding-token-0123456789"


def start_run() -> pb.StartRun:
    repository = pb.Repository(project="CP", name="acme/portal", clone_url=CLONE_URL)
    return pb.StartRun(
        tools=[
            pb.ToolBinding(
                binding="repo",
                mcp_url="http://x/mcp",
                scopes=["read", "git.write"],
                access="write",
                allow_tools=["create_issue_branch"],
                repository=repository,
            ),
            pb.ToolBinding(
                binding="issues", mcp_url="http://x/mcp", scopes=["read"], access="read", allow_tools=["get_issue"]
            ),
        ]
    )


class Tokens:
    """The channel's side of tokens, as Tokens.token answers once a grant is opened."""

    def __init__(self) -> None:
        self.asked: list[str] = []

    async def token(self, binding: str) -> str:
        self.asked.append(binding)
        return TOKEN


async def emit(kind: str, node: str, payload: dict[str, Any]) -> None:
    return None


async def test_a_repository_binding_gives_its_clone_address_and_token() -> None:
    bindings, tokens = bindings_from(start_run().tools), Tokens()
    assert bindings["issues"].repository is None
    ctx = RunContext(
        spec=parse(FIXTURE.read_text()), tools=FakeTools({}), emit=emit, repositories=Repositories(bindings, tokens)
    )
    access = await ctx.repository("repo")
    assert (access.url, access.token, access.writes) == (CLONE_URL, TOKEN, True)
    assert (access.repository.project, access.repository.name) == ("CP", "acme/portal")
    assert access.headers() == {"Authorization": f"Bearer {TOKEN}"}
    assert TOKEN not in repr(access), "printing the access prints the token"
    assert tokens.asked == ["repo"]
    assert ctx.repositories is not None and ctx.repositories.names() == ["repo"]


async def test_only_a_repository_binding_gives_a_token_for_git() -> None:
    tokens = Tokens()
    ctx = RunContext(
        spec=parse(FIXTURE.read_text()),
        tools=FakeTools({}),
        emit=emit,
        repositories=Repositories(bindings_from(start_run().tools), tokens),
    )
    for binding in ("issues", "nothing"):
        with pytest.raises(ToolRefusedError):
            await ctx.repository(binding)
    with pytest.raises(ToolRefusedError):
        await RunContext(spec=parse(FIXTURE.read_text()), tools=FakeTools({}), emit=emit).repository("repo")
    assert tokens.asked == []


class Unwrapper:
    async def unwrap(self, wrapping_token: str) -> dict[str, Any]:
        return {"access_token": TOKEN}


async def test_the_repositorys_token_is_cut_from_what_the_runner_sends() -> None:
    channel = Channel("localhost:1", b"", "bootstrap", "http://openbao")
    channel.unwrapper = Unwrapper()  # type: ignore[assignment]
    sent: list[pb.ConnectRequest] = []
    channel.send = sent.append  # type: ignore[method-assign,assignment]
    repositories = Repositories(bindings_from(start_run().tools), channel.tokens)

    async def grant() -> None:
        while not sent:
            await anyio.sleep(0)
        channel.tokens.deliver(pb.TokenGrant(binding="repo", wrapping_token="wrapped"))

    async with anyio.create_task_group() as group:
        group.start_soon(grant)
        access = await repositories.access("repo")
    assert access.token == TOKEN
    assert sent[0].token_request.binding == "repo"
    await channel.emit("log", "code", {"text": f"git push with {TOKEN}"})
    event = json.loads(sent[-1].event.payload_json)
    assert TOKEN not in event["text"] and REDACTED in event["text"]
