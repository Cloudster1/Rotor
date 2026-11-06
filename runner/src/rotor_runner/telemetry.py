"""The run's spans (#37, #241): OpenInference's for every chain, node, tool and model call and Rotor's own, on the run's
tracer provider, leave over the runner's channel as spans.py holds them, so the sandbox reaches no collector."""

from __future__ import annotations

import asyncio
from collections.abc import Callable, Iterator, Mapping
from contextlib import contextmanager, nullcontext
from typing import Any, cast

from langchain_core.callbacks import BaseCallbackHandler, BaseCallbackManager
from langchain_core.runnables.config import var_child_runnable_config
from langgraph.errors import GraphBubbleUp
from openinference.instrumentation import OITracer, TraceConfig
from opentelemetry import trace
from opentelemetry.context import Context
from opentelemetry.sdk.trace import ReadableSpan, SpanProcessor, TracerProvider
from opentelemetry.trace import Span, Status, StatusCode
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

from rotor_runner.config import SPAN_BATCH, SPAN_BATCH_BYTES, SPAN_FLUSH_S, SPAN_FLUSH_TIMEOUT_MS
from rotor_runner.langchain_tracer import OpenInferenceTracer, handler
from rotor_runner.pb.rotor.runner.v1 import runner_pb2 as pb
from rotor_runner.spans import KIND, NODE_TYPE, to_message

TRACER = "rotor_runner"
LANGCHAIN_TRACER = "openinference.instrumentation.langchain"
RUN = "rotor.run"
INTERRUPTED = "rotor.interrupted"
CHAIN, TOOL = "CHAIN", "TOOL"


def trace_config(content: bool) -> TraceConfig:
    """OpenInference's view of a run: never images or vectors, and no inputs, outputs or messages without content."""
    hide = not content
    return TraceConfig(
        hide_inputs=hide,
        hide_outputs=hide,
        hide_input_messages=hide,
        hide_output_messages=hide,
        hide_input_text=hide,
        hide_output_text=hide,
        hide_prompts=hide,
        hide_choices=hide,
        hide_llm_tools=hide,
        hide_retrieval_documents=hide,
        hide_embeddings_text=hide,
        hide_input_images=True,
        hide_embeddings_vectors=True,
    )


class RunAttributes(SpanProcessor):
    """Stamps every span with its run as it starts, OpenInference's among them; the control plane stamps it again."""

    def __init__(self, run: str) -> None:
        self._run = run

    def on_start(self, span: Any, parent_context: Context | None = None) -> None:
        if self._run:
            span.set_attribute(RUN, self._run)

    def on_end(self, span: ReadableSpan) -> None:
        return None

    def shutdown(self) -> None:
        return None

    def force_flush(self, timeout_millis: int = SPAN_FLUSH_TIMEOUT_MS) -> bool:
        return True


class ChannelSpanProcessor(SpanProcessor):
    """Hands each finished span to the event loop, which holds and sends them in batches over the channel; a span may
    end on LangChain's handler thread, so the loop alone redacts, touches the buffer and sends."""

    def __init__(
        self,
        loop: asyncio.AbstractEventLoop,
        send: Callable[[pb.ConnectRequest], None],
        scrub: Callable[[str], str],
        content: bool = False,
    ):
        self._loop, self._send, self._scrub, self._content = loop, send, scrub, content
        self._buffer: list[pb.RunnerSpan] = []
        self._bytes = 0
        self._timer: asyncio.TimerHandle | None = None

    def on_start(self, span: Any, parent_context: Context | None = None) -> None:
        return None

    def on_end(self, span: ReadableSpan) -> None:
        self._loop.call_soon_threadsafe(self._add, span)

    def _add(self, span: ReadableSpan) -> None:
        message = to_message(span, self._scrub, self._content)
        size = message.ByteSize()
        if self._buffer and self._bytes + size > SPAN_BATCH_BYTES:
            self.send_now()
        self._buffer.append(message)
        self._bytes += size
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
            self._buffer, self._bytes = [], 0

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
        run: str = "",
    ) -> None:
        self.content = content
        self._processor: ChannelSpanProcessor | None = None
        self._root: Context | None = None
        self._tracer: trace.Tracer = trace.NoOpTracer()
        self._langchain: OpenInferenceTracer | None = None
        if not traceparent or send is None:
            return
        root = TraceContextTextMapPropagator().extract({"traceparent": traceparent})
        parent = trace.get_current_span(root).get_span_context()
        if not parent.is_valid or not parent.trace_flags.sampled:
            return
        self._processor = ChannelSpanProcessor(asyncio.get_running_loop(), send, scrub or (lambda text: text), content)
        provider = TracerProvider()
        provider.add_span_processor(RunAttributes(run))
        provider.add_span_processor(self._processor)
        self._tracer, self._root = provider.get_tracer(TRACER), root
        tracer = OITracer(provider.get_tracer(LANGCHAIN_TRACER), config=trace_config(content))
        self._langchain = handler(cast(trace.Tracer, tracer))

    @property
    def on(self) -> bool:
        return self._processor is not None

    def _langchain_span(self) -> Span | None:
        """The span of the LangChain run the caller is in, read from the config LangChain keeps current for it."""
        if self._langchain is None:
            return None
        manager = (var_child_runnable_config.get() or {}).get("callbacks")
        if not isinstance(manager, BaseCallbackManager) or manager.parent_run_id is None:
            return None
        return self._langchain.get_span(manager.parent_run_id)

    @contextmanager
    def span(
        self, name: str, attributes: Mapping[str, Any] | None = None, *, kind: str = CHAIN, root: bool = False
    ) -> Iterator[Span]:
        """A span around a block, current within it: under the run's traceparent when root, else under the LangChain
        run the block is in, so it nests where LangChain's callbacks do, else under the current span."""
        langchain = None if root else self._langchain_span()
        context = self._root if root else trace.set_span_in_context(langchain) if langchain is not None else None
        with self._tracer.start_as_current_span(
            name,
            context=context,
            attributes={KIND: kind, **(attributes or {})},
            record_exception=False,
            set_status_on_exception=False,
        ) as span:
            try:
                yield span
            except GraphBubbleUp:
                # An interrupt waits for a person; it is how an approval works, not a failure.
                span.set_attribute(INTERRUPTED, True)
                raise
            except BaseException as err:
                span.set_status(Status(StatusCode.ERROR, str(err)))
                raise

    def headers(self) -> dict[str, str]:
        """The traceparent of the span current now, sent with each MCP request so a tool that traces with OpenTelemetry
        continues the run's trace (#243); nothing when the run is not traced."""
        carrier: dict[str, str] = {}
        if self.on:
            TraceContextTextMapPropagator().inject(carrier)
        return carrier

    def maybe(self, name: str, attributes: Mapping[str, Any] | None = None, *, kind: str = CHAIN) -> Any:
        """span when tracing is on, else nothing to enter at all."""
        return self.span(name, attributes, kind=kind) if self.on else nullcontext()

    def root(self, name: str, attributes: Mapping[str, Any] | None = None) -> Any:
        """The run's own span, a child of the run's traceparent, when tracing is on."""
        return self.span(name, attributes, root=True) if self.on else nullcontext()

    @contextmanager
    def node(self, node_id: str, node_type: str) -> Iterator[None]:
        """A node's span: the one OpenInference made for the LangGraph node, named as Rotor names nodes, or Rotor's
        own where LangChain's callbacks made none."""
        attributes = {"rotor.node": node_id, NODE_TYPE: node_type}
        span = self._langchain_span()
        if span is None:
            with self.span(f"node {node_id}", attributes):
                yield
            return
        span.update_name(f"node {node_id}")
        span.set_attributes(attributes)
        try:
            yield
        except GraphBubbleUp:
            span.set_attribute(INTERRUPTED, True)
            raise

    def callbacks(self) -> list[BaseCallbackHandler]:
        return [self._langchain] if self._langchain is not None else []

    async def flush(self) -> None:
        """Sends every span ended so far; the loop first runs the hand-overs other threads queued."""
        if self._processor is not None:
            await asyncio.sleep(0)
            self._processor.send_now()
