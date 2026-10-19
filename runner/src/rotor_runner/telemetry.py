"""The run's spans (#37): they join the run's traceparent and leave over the runner's channel, so the sandbox reaches
no collector; every attribute passes the runner's redactor first, and the control plane checks them again."""

from __future__ import annotations

import asyncio
from collections.abc import Callable, Iterator, Mapping
from contextlib import contextmanager, nullcontext
from typing import Any
from uuid import UUID

from langchain_core.callbacks import AsyncCallbackHandler
from langchain_core.messages import BaseMessage
from langchain_core.outputs import LLMResult
from langgraph.errors import GraphBubbleUp
from opentelemetry import context as otel_context
from opentelemetry import trace
from opentelemetry.context import Context
from opentelemetry.sdk.trace import ReadableSpan, SpanProcessor, TracerProvider
from opentelemetry.trace import Span, Status, StatusCode
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

from rotor_runner.config import (
    SPAN_ATTR_CHARS,
    SPAN_BATCH,
    SPAN_FLUSH_S,
    SPAN_FLUSH_TIMEOUT_MS,
    SPAN_ID_BYTES,
    SPAN_PROMPT_MESSAGES,
    TRACE_ID_BYTES,
)
from rotor_runner.pb.rotor.runner.v1 import runner_pb2 as pb

TRACER = "rotor_runner"


def to_message(span: ReadableSpan, scrub: Callable[[str], str]) -> pb.RunnerSpan:
    """A finished span as the runner protocol carries it, every value as redacted text."""
    ctx = span.get_span_context() or trace.INVALID_SPAN_CONTEXT
    message = pb.RunnerSpan(
        trace_id=ctx.trace_id.to_bytes(TRACE_ID_BYTES, "big"),
        span_id=ctx.span_id.to_bytes(SPAN_ID_BYTES, "big"),
        name=span.name,
        error=span.status.status_code == StatusCode.ERROR,
        status_message=scrub(span.status.description or "")[:SPAN_ATTR_CHARS],
    )
    if span.parent is not None:
        message.parent_span_id = span.parent.span_id.to_bytes(SPAN_ID_BYTES, "big")
    message.start.FromNanoseconds(span.start_time or 0)
    message.end.FromNanoseconds(span.end_time or 0)
    for key, value in (span.attributes or {}).items():
        message.attributes[key] = scrub(value if isinstance(value, str) else str(value))[:SPAN_ATTR_CHARS]
    return message


class ChannelSpanProcessor(SpanProcessor):
    """Hands each finished span to the event loop, which sends them in batches over the channel; a span may end on
    another thread, so the loop alone touches the buffer and the channel."""

    def __init__(
        self, loop: asyncio.AbstractEventLoop, send: Callable[[pb.ConnectRequest], None], scrub: Callable[[str], str]
    ):
        self._loop, self._send, self._scrub = loop, send, scrub
        self._buffer: list[pb.RunnerSpan] = []
        self._timer: asyncio.TimerHandle | None = None

    def on_start(self, span: Any, parent_context: Context | None = None) -> None:
        return None

    def on_end(self, span: ReadableSpan) -> None:
        self._loop.call_soon_threadsafe(self._add, to_message(span, self._scrub))

    def _add(self, message: pb.RunnerSpan) -> None:
        self._buffer.append(message)
        if len(self._buffer) >= SPAN_BATCH:
            self.send_now()
        elif self._timer is None:
            self._timer = self._loop.call_later(SPAN_FLUSH_S, self.send_now)

    def send_now(self) -> None:
        if self._timer is not None:
            self._timer.cancel()
            self._timer = None
        if self._buffer:
            self._send(pb.ConnectRequest(spans=pb.SpanBatch(spans=self._buffer)))
            self._buffer = []

    def shutdown(self) -> None:
        return None

    def force_flush(self, timeout_millis: int = SPAN_FLUSH_TIMEOUT_MS) -> bool:
        return True


class RunTracing:
    """A run's tracer: without a sampled traceparent and a channel it makes no spans at all, at no cost."""

    def __init__(
        self,
        traceparent: str = "",
        send: Callable[[pb.ConnectRequest], None] | None = None,
        scrub: Callable[[str], str] | None = None,
        content: bool = False,
    ) -> None:
        self.content = content
        self._processor: ChannelSpanProcessor | None = None
        self._root: Context | None = None
        self._tracer: trace.Tracer = trace.NoOpTracer()
        if not traceparent or send is None:
            return
        root = TraceContextTextMapPropagator().extract({"traceparent": traceparent})
        parent = trace.get_current_span(root).get_span_context()
        if not parent.is_valid or not parent.trace_flags.sampled:
            return
        self._processor = ChannelSpanProcessor(asyncio.get_running_loop(), send, scrub or (lambda text: text))
        provider = TracerProvider()
        provider.add_span_processor(self._processor)
        self._tracer, self._root = provider.get_tracer(TRACER), root

    @property
    def on(self) -> bool:
        return self._processor is not None

    @contextmanager
    def span(self, name: str, attributes: Mapping[str, Any] | None = None, *, root: bool = False) -> Iterator[Span]:
        """A span around a block, current within it: under the run's traceparent when root, else under the current."""
        with self._tracer.start_as_current_span(
            name,
            context=self._root if root else None,
            attributes=dict(attributes or {}),
            record_exception=False,
            set_status_on_exception=False,
        ) as span:
            try:
                yield span
            except GraphBubbleUp:
                # An interrupt waits for a person; it is how an approval works, not a failure.
                span.set_attribute("rotor.interrupted", True)
                raise
            except BaseException as err:
                span.set_status(Status(StatusCode.ERROR, str(err)))
                raise

    def maybe(self, name: str, attributes: Mapping[str, Any] | None = None) -> Any:
        """span when tracing is on, else nothing to enter at all."""
        return self.span(name, attributes) if self.on else nullcontext()

    def root(self, name: str, attributes: Mapping[str, Any] | None = None) -> Any:
        """The run's own span, a child of the run's traceparent, when tracing is on."""
        return self.span(name, attributes, root=True) if self.on else nullcontext()

    def start(self, name: str, attributes: Mapping[str, Any] | None = None) -> Span:
        """A span under the current one that the caller ends, for work that begins and ends in different callbacks."""
        return self._tracer.start_span(name, context=otel_context.get_current(), attributes=dict(attributes or {}))

    def callbacks(self) -> list[AsyncCallbackHandler]:
        return [LlmSpans(self)] if self.on else []

    async def flush(self) -> None:
        """Sends every span ended so far; the loop first runs the hand-overs other threads queued."""
        if self._processor is not None:
            await asyncio.sleep(0)
            self._processor.send_now()


def _text(message: BaseMessage) -> str:
    content = message.content
    return content if isinstance(content, str) else str(content)


class LlmSpans(AsyncCallbackHandler):
    """A span per model call with its model and tokens; the prompt and the answer only when the run may carry them,
    for LangSmith, and cut short."""

    def __init__(self, tracing: RunTracing) -> None:
        self.tracing = tracing
        self.open: dict[UUID, Span] = {}

    async def on_chat_model_start(
        self, serialized: dict[str, Any], messages: list[list[BaseMessage]], *, run_id: UUID, **kwargs: Any
    ) -> None:
        params = kwargs.get("invocation_params") or {}
        model = str(params.get("model") or params.get("model_name") or (serialized or {}).get("name") or "model")
        attributes: dict[str, Any] = {"gen_ai.request.model": model, "langsmith.span.kind": "llm"}
        if self.tracing.content:
            flat = [m for group in messages for m in group]
            for i, message in enumerate(flat[-SPAN_PROMPT_MESSAGES:]):
                attributes[f"gen_ai.prompt.{i}.role"] = message.type
                attributes[f"gen_ai.prompt.{i}.content"] = _text(message)[:SPAN_ATTR_CHARS]
        self.open[run_id] = self.tracing.start(f"llm {model}", attributes)

    async def on_llm_end(self, response: LLMResult, *, run_id: UUID, **_kwargs: Any) -> None:
        span = self.open.pop(run_id, None)
        if span is None:
            return
        inputs = outputs = 0
        answer = ""
        for generations in response.generations:
            for generation in generations:
                message = getattr(generation, "message", None)
                usage = getattr(message, "usage_metadata", None) or {}
                inputs += int(usage.get("input_tokens", 0))
                outputs += int(usage.get("output_tokens", 0))
                answer = generation.text or answer
        span.set_attribute("gen_ai.usage.input_tokens", inputs)
        span.set_attribute("gen_ai.usage.output_tokens", outputs)
        if self.tracing.content and answer:
            span.set_attribute("gen_ai.completion.0.role", "assistant")
            span.set_attribute("gen_ai.completion.0.content", answer[:SPAN_ATTR_CHARS])
        span.end()

    async def on_llm_error(self, error: BaseException, *, run_id: UUID, **_kwargs: Any) -> None:
        span = self.open.pop(run_id, None)
        if span is not None:
            span.set_status(Status(StatusCode.ERROR, str(error)))
            span.end()
