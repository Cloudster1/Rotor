"""A finished span as the runner sends it (#37, #241): text only when the run may keep it, every value redacted, and all
held to the control plane's limits, so what leaves the sandbox is already what the control plane keeps."""

from __future__ import annotations

import json
import re
from collections.abc import Callable
from importlib import resources
from typing import Any

from opentelemetry import trace
from opentelemetry.sdk.trace import Event, ReadableSpan
from opentelemetry.trace import StatusCode

from rotor_runner.config import (
    SPAN_ATTR_CHARS,
    SPAN_ATTRIBUTES,
    SPAN_BYTES,
    SPAN_CONTENT_CHARS,
    SPAN_EVENTS,
    SPAN_ID_BYTES,
    SPAN_NAME_CHARS,
    SPAN_PROMPT_MESSAGES,
    SPAN_STACK_CHARS,
    TRACE_ID_BYTES,
)
from rotor_runner.pb.rotor.runner.v1 import runner_pb2 as pb

CONTENT: dict[str, Any] = json.loads(resources.files("rotor_runner").joinpath("trace-content.json").read_text())

KIND = "openinference.span.kind"
NODE_TYPE = "rotor.node.type"
CONTENT_CUT = "rotor.content_cut"
MESSAGES_OMITTED = "rotor.messages_omitted"
STACK = "exception.stacktrace"
# What OpenInference writes where its TraceConfig hides a value; the runner drops the key rather than send it.
HIDDEN = "__REDACTED__"
# OpenInference's names for a model call's model and tokens, copied to the gen_ai keys #37's readers know.
GEN_AI = {
    "llm.model_name": "gen_ai.request.model",
    "llm.token_count.prompt": "gen_ai.usage.input_tokens",
    "llm.token_count.completion": "gen_ai.usage.output_tokens",
}
# LangGraph's control flow, such as an approval's interrupt, which OpenInference records as if it were a failure.
CONTROL_FLOW = frozenset({"GraphInterrupt", "NodeInterrupt", "GraphBubbleUp", "ParentCommand"})
_INPUT_MESSAGE = re.compile(r"llm\.input_messages\.(\d+)")

Scrub = Callable[[str], str]


def _under(key: str, name: str) -> bool:
    return key == name or key.startswith(name + ".")


def is_text(key: str) -> bool:
    """Whether an attribute carries text a person, a tool or a model wrote, as api/trace-content.json says."""
    if any(_under(key, name) for name in CONTENT["text"]):
        return True
    return not any(_under(key, name) for name in CONTENT["plain"])


def never(key: str) -> bool:
    """Whether an attribute is an image or an embedding vector, which no span carries."""
    padded = f".{key}."
    return any(f".{name}." in padded for name in CONTENT["never"])


def _string(value: Any) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, bool | list | tuple):
        return json.dumps(list(value) if isinstance(value, tuple) else value, default=str)
    return str(value)


def _size(text: str) -> int:
    return len(text.encode())


def _last_messages(attrs: dict[str, str]) -> dict[str, str]:
    """A model call's last SPAN_PROMPT_MESSAGES input messages, numbered from 0 again, and how many were left out."""
    indices = sorted({int(m.group(1)) for key in attrs if (m := _INPUT_MESSAGE.match(key))})
    if len(indices) <= SPAN_PROMPT_MESSAGES:
        return attrs
    kept = {old: new for new, old in enumerate(indices[-SPAN_PROMPT_MESSAGES:])}
    out: dict[str, str] = {}
    for key, value in attrs.items():
        m = _INPUT_MESSAGE.match(key)
        if m is None:
            out[key] = value
        elif (index := int(m.group(1))) in kept:
            out[f"llm.input_messages.{kept[index]}{key[m.end() :]}"] = value
    out[MESSAGES_OMITTED] = str(len(indices) - SPAN_PROMPT_MESSAGES)
    return out


def _rotor_view(span: ReadableSpan) -> dict[str, str]:
    """The span's attributes as strings, with what Rotor adds: a node's kind, and a model call's gen_ai keys, which a
    finished span can no longer take."""
    attrs = {key: _string(value) for key, value in (span.attributes or {}).items()}
    if (node_type := attrs.get(NODE_TYPE)) is not None:
        attrs[KIND] = "AGENT" if node_type == "agent" else "CHAIN"
    for source, target in GEN_AI.items():
        if source in attrs:
            attrs[target] = attrs[source]
    if attrs.get(KIND) == "LLM":
        # A model that names none, as a test's does, is known by its class, which names the span.
        attrs.setdefault(GEN_AI["llm.model_name"], span.name)
    return _last_messages(attrs)


def _rank(key: str) -> int:
    return 0 if _under(key, "rotor") else 2 if is_text(key) else 1


def held_attributes(attrs: dict[str, str], content: bool, scrub: Scrub) -> dict[str, str]:
    """Attributes redacted and cut to their limits, text only with content; Rotor's first, then those without text,
    then text, each in key order, up to SPAN_ATTRIBUTES."""
    kept: dict[str, str] = {}
    for key, value in attrs.items():
        text = is_text(key)
        if never(key) or value == HIDDEN or (text and not content):
            continue
        kept[scrub(key)[:SPAN_NAME_CHARS]] = scrub(value)[: SPAN_CONTENT_CHARS if text else SPAN_ATTR_CHARS]
    return {key: kept[key] for key in sorted(kept, key=lambda k: (_rank(k), k))[:SPAN_ATTRIBUTES]}


def _event(event: Event, content: bool, scrub: Scrub) -> pb.RunnerSpanEvent:
    out = pb.RunnerSpanEvent(name=scrub(event.name)[:SPAN_NAME_CHARS])
    out.time.FromNanoseconds(event.timestamp)
    for key, value in (event.attributes or {}).items():
        text = is_text(key)
        if never(key) or (text and not content):
            continue
        limit = SPAN_STACK_CHARS if key == STACK else SPAN_CONTENT_CHARS if text else SPAN_ATTR_CHARS
        out.attributes[scrub(key)[:SPAN_NAME_CHARS]] = scrub(_string(value))[:limit]
    return out


def held_events(span: ReadableSpan, content: bool, scrub: Scrub) -> list[pb.RunnerSpanEvent]:
    """The span's last SPAN_EVENTS events, without those LangGraph's control flow raised."""
    events = [e for e in span.events if (e.attributes or {}).get("exception.type") not in CONTROL_FLOW]
    return [_event(e, content, scrub) for e in events[-SPAN_EVENTS:]]


def _span_bytes(message: pb.RunnerSpan) -> int:
    events = sum(_size(e.name) + sum(_size(k) + _size(v) for k, v in e.attributes.items()) for e in message.events)
    return _size(message.name) + sum(_size(k) + _size(v) for k, v in message.attributes.items()) + events


def fit(message: pb.RunnerSpan) -> None:
    """Holds a span to SPAN_BYTES: drops its text, longest first, then its oldest events, then its longest other
    attributes but Rotor's, and says so in rotor.content_cut."""
    size = _span_bytes(message)
    if size <= SPAN_BYTES:
        return
    size += _size(CONTENT_CUT) + _size("true")
    attrs = message.attributes

    def drop(keys: list[str], left: int) -> int:
        for key in sorted(keys, key=lambda k: -_size(attrs[k])):
            if left <= SPAN_BYTES:
                break
            left -= _size(key) + _size(attrs[key])
            del attrs[key]
        return left

    size = drop([k for k in attrs if _rank(k) == 2], size)
    while size > SPAN_BYTES and message.events:
        oldest = message.events[0]
        size -= _size(oldest.name) + sum(_size(k) + _size(v) for k, v in oldest.attributes.items())
        del message.events[0]
    drop([k for k in attrs if _rank(k) == 1], size)
    attrs[CONTENT_CUT] = "true"


def _status(span: ReadableSpan, scrub: Scrub) -> str:
    # LangChain's error is its repr, a blank line, then the stack, which the span's exception event carries already.
    return scrub(span.status.description or "").split("\n\n", 1)[0][:SPAN_ATTR_CHARS]


def to_message(span: ReadableSpan, scrub: Scrub, content: bool = False) -> pb.RunnerSpan:
    """A finished span as the runner protocol carries it, every value redacted text held to its limits."""
    ctx = span.get_span_context() or trace.INVALID_SPAN_CONTEXT
    message = pb.RunnerSpan(
        trace_id=ctx.trace_id.to_bytes(TRACE_ID_BYTES, "big"),
        span_id=ctx.span_id.to_bytes(SPAN_ID_BYTES, "big"),
        name=scrub(span.name)[:SPAN_NAME_CHARS],
        error=span.status.status_code == StatusCode.ERROR,
        status_message=_status(span, scrub),
        attributes=held_attributes(_rotor_view(span), content, scrub),
        events=held_events(span, content, scrub),
    )
    if span.parent is not None:
        message.parent_span_id = span.parent.span_id.to_bytes(SPAN_ID_BYTES, "big")
    message.start.FromNanoseconds(span.start_time or 0)
    message.end.FromNanoseconds(span.end_time or 0)
    fit(message)
    return message
