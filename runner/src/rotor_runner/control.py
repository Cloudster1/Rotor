"""The runner's side of the runner protocol: it dials the control plane, numbers what it sends and replays what was
not acknowledged after a reconnect, heartbeats, and asks for tokens as its nodes need them."""

from __future__ import annotations

import asyncio
import contextlib
import json
import logging
import time
import uuid
from collections.abc import AsyncIterator
from typing import Any

import grpc
from langchain_core.language_models import BaseChatModel
from langgraph.checkpoint.base import BaseCheckpointSaver

from rotor_runner import __version__
from rotor_runner.checkpoint import CheckpointRefusedError, RemoteSaver
from rotor_runner.codenode import CodeNodeRunner
from rotor_runner.compile import Outcome, RunContext, run_workflow
from rotor_runner.config import (
    DEFAULT_TOOL_CALLS_PER_MINUTE,
    EXIT_REFUSED,
    HEARTBEAT_INTERVAL_S,
    MAX_MESSAGE_BYTES,
    RECONNECT_MAX_S,
    RECONNECT_MIN_S,
    TOKEN_REFRESH_BEFORE_S,
    TOKEN_WAIT_S,
)
from rotor_runner.limits import ToolRate
from rotor_runner.mcp import Binding, McpGateway, ToolFailedError
from rotor_runner.models import build_model
from rotor_runner.pb.rotor.runner.v1 import runner_pb2 as pb
from rotor_runner.pb.rotor.runner.v1 import runner_pb2_grpc as pb_grpc
from rotor_runner.redact import Redactor
from rotor_runner.spec import parse
from rotor_runner.telemetry import RunTracing
from rotor_runner.tokens import UnwrapError, Unwrapper

log = logging.getLogger("rotor_runner")

OUTCOMES = {
    "succeeded": pb.RUN_OUTCOME_SUCCEEDED,
    "failed": pb.RUN_OUTCOME_FAILED,
    "cancelled": pb.RUN_OUTCOME_CANCELLED,
    "interrupted": pb.RUN_OUTCOME_INTERRUPTED,
}


class Refused(Exception):  # noqa: N818 - the control plane's answer, not this runner's error
    """The control plane refused this runner's credential; nothing will change that by retrying."""


class Tokens:
    """The run's access tokens, in memory alone, asked for over the channel before each runs out."""

    def __init__(self, channel: Channel) -> None:
        self._channel = channel
        self._held: dict[str, tuple[str, float]] = {}
        self._waiting: dict[str, asyncio.Future[pb.TokenGrant]] = {}

    async def token(self, binding: str) -> str:
        held = self._held.get(binding)
        if held and held[1] - time.time() > TOKEN_REFRESH_BEFORE_S:
            return held[0]
        while True:
            future = self._waiting.get(binding)
            if future is None or future.done():
                future = asyncio.get_running_loop().create_future()
                self._waiting[binding] = future
                self._channel.send(pb.ConnectRequest(token_request=pb.TokenRequest(binding=binding)))
            try:
                grant = await asyncio.wait_for(asyncio.shield(future), TOKEN_WAIT_S)
            except TimeoutError:
                self._waiting.pop(binding, None)
                continue
            if grant.error:
                raise ToolFailedError(f"no token for {binding}: {grant.error}")
            if grant.WhichOneof("token") != "wrapping_token":
                raise ToolFailedError(f"the token for {binding} did not arrive wrapped, so the runner refuses it")
            try:
                opened = await self._channel.unwrapper.unwrap(grant.wrapping_token)
            except UnwrapError as err:
                raise ToolFailedError(f"no token for {binding}: {err}") from err
            token = str(opened.get("access_token", ""))
            self._channel.redactor.hold(token)
            self._held[binding] = (token, grant.expires_at.ToSeconds())
            return token

    def deliver(self, grant: pb.TokenGrant) -> None:
        future = self._waiting.pop(grant.binding, None)
        if future is not None and not future.done():
            future.set_result(grant)


class Channel:
    """One run's stream to the control plane, kept open across drops until the run's Done is acknowledged."""

    def __init__(
        self, address: str, ca_pem: bytes, bootstrap: str, openbao: str, redactor: Redactor | None = None
    ) -> None:
        self.address, self._ca, self._bootstrap = address, ca_pem, bootstrap
        self.unwrapper, self.redactor = Unwrapper(openbao), redactor or Redactor()
        self._opening: set[asyncio.Task[None]] = set()
        self._session: str | None = None
        self._next_seq, self._handled_control = 1, 0
        self._outbox: list[pb.ConnectRequest] = []
        self._queue: asyncio.Queue[pb.ConnectRequest | None] | None = None
        self._run: asyncio.Task[None] | None = None
        self._finished = asyncio.Event()
        self.tokens = Tokens(self)
        self._keys: dict[str, asyncio.Future[str]] = {}
        self.replies: dict[str, asyncio.Future[pb.ConnectResponse]] = {}
        self.checkpointer: BaseCheckpointSaver[Any] = RemoteSaver(self)
        self.tracing = RunTracing()

    def send(self, message: pb.ConnectRequest) -> None:
        """Sends now if connected; a numbered message also waits in the outbox until acknowledged."""
        message.ack = self._handled_control
        if self._queue is not None:
            self._queue.put_nowait(message)

    def send_numbered(self, message: pb.ConnectRequest) -> None:
        message.seq = self._next_seq
        self._next_seq += 1
        self._outbox.append(message)
        self.send(message)

    async def emit(self, kind: str, node: str, payload: dict[str, Any]) -> None:
        event = pb.Event(type=kind, node_id=node, payload_json=self.redactor.scrub(json.dumps(payload, default=str)))
        event.at.GetCurrentTime()
        self.send_numbered(pb.ConnectRequest(event=event))

    async def request(self, message: pb.ConnectRequest, request_id: str) -> pb.ConnectResponse:
        """Sends a request and waits for its reply, sending it again after each reconnect until one comes."""
        while True:
            future: asyncio.Future[pb.ConnectResponse] = asyncio.get_running_loop().create_future()
            self.replies[request_id] = future
            self.send(message)
            with contextlib.suppress(TimeoutError):
                return await asyncio.wait_for(future, TOKEN_WAIT_S)

    async def run(self) -> int:
        """Serves the run to its end; the exit code says whether it was refused."""
        pause, connected = RECONNECT_MIN_S, False
        while not self._finished.is_set():
            try:
                await self._connect()
                connected = True
                pause = RECONNECT_MIN_S
            except Refused:
                log.error("the control plane refused this runner's credential")
                return EXIT_REFUSED
            except grpc.aio.AioRpcError as err:
                log.warning("the stream to the control plane ended: %s", err.code())
            if not self._finished.is_set():
                await asyncio.sleep(pause)
                pause = min(pause * 2, RECONNECT_MAX_S)
        log.info("the run is done and the control plane knows it (connected before: %s)", connected)
        return 0

    async def _connect(self) -> None:
        credentials = grpc.ssl_channel_credentials(root_certificates=self._ca)
        options = [
            ("grpc.max_receive_message_length", MAX_MESSAGE_BYTES),
            ("grpc.max_send_message_length", MAX_MESSAGE_BYTES),
        ]
        async with grpc.aio.secure_channel(self.address, credentials, options=options) as channel:
            queue: asyncio.Queue[pb.ConnectRequest | None] = asyncio.Queue()
            hello = pb.Hello(runner_version=__version__)
            if self._session:
                hello.session_token = self._session
            else:
                hello.bootstrap_token = self._bootstrap
            queue.put_nowait(pb.ConnectRequest(hello=hello, ack=self._handled_control))
            call = pb_grpc.RunnerControlServiceStub(channel).Connect(_drain(queue))
            heartbeat = asyncio.create_task(self._heartbeat(queue))
            try:
                async for response in call:
                    if response.HasField("hello_ack"):
                        self._session = response.hello_ack.session_token
                        # A sandbox started for a resumed run numbers on from where the run's last sandbox stopped.
                        self._next_seq = max(self._next_seq, response.ack + 1)
                        self._acknowledged(response.ack)
                        self._queue = queue
                        for pending in self._outbox:
                            pending.ack = self._handled_control
                            queue.put_nowait(pending)
                        continue
                    self._handle(response)
                    if self._finished.is_set():
                        queue.put_nowait(None)
            except grpc.aio.AioRpcError as err:
                if err.code() == grpc.StatusCode.UNAUTHENTICATED and self._session is None:
                    raise Refused from err
                raise
            finally:
                self._queue = None
                heartbeat.cancel()

    def _acknowledged(self, ack: int) -> None:
        self._outbox = [m for m in self._outbox if m.seq > ack]
        if self._done_sent and not self._outbox:
            self._finished.set()

    @property
    def _done_sent(self) -> bool:
        return self._run is not None and self._run.done()

    def _handle(self, response: pb.ConnectResponse) -> None:
        self._acknowledged(response.ack)
        kind = response.WhichOneof("message")
        if kind is None:
            return
        if response.seq:
            if response.seq <= self._handled_control:
                self.send(pb.ConnectRequest())
                return
            self._handled_control = response.seq
        if kind == "start_run" and self._run is None:
            self._run = asyncio.create_task(self._execute(response.start_run))
        elif kind == "cancel" and self._run is not None:
            self._run.cancel()
        elif kind == "token_grant":
            self.tokens.deliver(response.token_grant)
        elif kind == "secret_grant":
            opening = asyncio.create_task(self._open_secret(response.secret_grant))
            self._opening.add(opening)
            opening.add_done_callback(self._opening.discard)
        elif kind.startswith("checkpoint_"):
            reply = getattr(response, kind)
            future = self.replies.pop(reply.request_id, None)
            if future is not None and not future.done():
                future.set_result(response)
        if response.seq:
            self.send(pb.ConnectRequest())

    async def _open_secret(self, grant: pb.SecretGrant) -> None:
        awaited = self._key(grant.provider_id)
        if awaited.done() or grant.WhichOneof("secret") != "wrapping_token":
            return
        try:
            key = str((await self.unwrapper.unwrap(grant.wrapping_token)).get("api_key", ""))
        except UnwrapError as err:
            log.error("the provider key could not be unwrapped: %s", err)
            return
        self.redactor.hold(key)
        if not awaited.done():
            awaited.set_result(key)

    def _key(self, provider_id: str) -> asyncio.Future[str]:
        if provider_id not in self._keys:
            self._keys[provider_id] = asyncio.get_running_loop().create_future()
        return self._keys[provider_id]

    async def _heartbeat(self, queue: asyncio.Queue[pb.ConnectRequest | None]) -> None:
        while True:
            await asyncio.sleep(HEARTBEAT_INTERVAL_S)
            queue.put_nowait(pb.ConnectRequest(heartbeat=pb.Heartbeat(), ack=self._handled_control))

    async def _execute(self, start: pb.StartRun) -> None:
        self.tracing = RunTracing(start.traceparent, self.send, self.redactor.scrub, start.trace_content)
        try:
            outcome = await self._run_workflow(start)
        except asyncio.CancelledError:
            outcome = Outcome("cancelled", "cancelled")
        except CheckpointRefusedError as err:
            outcome = Outcome("failed", "checkpoint_refused", {"message": str(err)})
        except Exception as err:
            log.exception("the run broke")
            outcome = Outcome("failed", "runner_error", {"message": str(err)})
        # The spans go before Done, so the control plane has them while the stream is still open.
        await self.tracing.flush()
        done = pb.Done(
            outcome=OUTCOMES[outcome.status],
            reason=self.redactor.scrub(outcome.reason),
            output_json=self.redactor.scrub(json.dumps(outcome.interrupt or outcome.output, default=str)),
        )
        self.send_numbered(pb.ConnectRequest(done=done))

    async def _run_workflow(self, start: pb.StartRun) -> Outcome:
        spec = parse(start.spec_yaml)
        bindings = {
            b.binding: Binding(
                b.binding, b.mcp_url, tuple(b.scopes), b.access, tuple(b.allow_tools), tuple(b.link_hosts)
            )
            for b in start.tools
        }
        model = None
        if start.HasField("model") and start.model.model:
            # The key arrives in a SecretGrant right after StartRun, and after every reconnect.
            key = await asyncio.wait_for(self._key(start.model.provider_id), TOKEN_WAIT_S)
            config = start.model

            def model() -> BaseChatModel:
                return build_model(config, key)

        ctx = RunContext(
            spec=spec,
            tools=McpGateway(
                bindings=bindings,
                tokens=self.tokens,
                emit=self.emit,
                rate=ToolRate(int(spec.limits.get("toolCallsPerMinute") or DEFAULT_TOOL_CALLS_PER_MINUTE)),
                tracing=self.tracing,
            ),
            emit=self.emit,
            model=model,
            code=CodeNodeRunner(),
            tracing=self.tracing,
            link_hosts=frozenset(h for b in bindings.values() for h in b.link_hosts),
        )
        inputs = json.loads(start.inputs_json or "{}")
        resume = json.loads(start.resume_json) if start.resume else None
        return await run_workflow(
            ctx,
            inputs,
            thread_id=start.run_id,
            checkpointer=self.checkpointer,
            resuming=start.resume,
            resume=resume,
            resume_id=start.resume_interrupt_id,
        )


async def _drain(queue: asyncio.Queue[pb.ConnectRequest | None]) -> AsyncIterator[pb.ConnectRequest]:
    while True:
        message = await queue.get()
        if message is None:
            return
        yield message


def request_id() -> str:
    return uuid.uuid4().hex
