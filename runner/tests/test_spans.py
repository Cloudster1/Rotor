"""How a span is held before it leaves the sandbox (#241): which keys carry text, the limits of the contract, a model
call's last messages, and a span cut down to its size."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from opentelemetry.sdk.trace import Event, ReadableSpan
from opentelemetry.trace import SpanContext

from rotor_runner.config import (
    SPAN_ATTR_CHARS,
    SPAN_ATTRIBUTES,
    SPAN_BYTES,
    SPAN_CONTENT_CHARS,
    SPAN_EVENTS,
    SPAN_PROMPT_MESSAGES,
    SPAN_STACK_CHARS,
)
from rotor_runner.spans import CONTENT, is_text, never, to_message

ROOT = Path(__file__).resolve().parents[2]
SECRET = "stub_oat_the-run-token-1234"


def scrub(text: str) -> str:
    return text.replace(SECRET, "[redacted]")


def span(attributes: dict[str, Any], events: tuple[Event, ...] = (), name: str = "ChatModel") -> ReadableSpan:
    return ReadableSpan(
        name=name,
        context=SpanContext(trace_id=1, span_id=2, is_remote=False),
        attributes=attributes,
        events=events,
        start_time=1,
        end_time=2,
    )


def test_the_packaged_trace_content_is_the_one_in_api() -> None:
    assert json.loads((ROOT / "api" / "trace-content.json").read_text()) == CONTENT, "run make trace-content"


def test_keys_that_carry_text_are_told_from_those_that_do_not() -> None:
    for key in ("input.value", "llm.input_messages.0.message.content", "tool.parameters", "llm.prompts", "custom"):
        assert is_text(key), key
    for key in ("openinference.span.kind", "llm.model_name", "tool.name", "tool_call.id", "metadata", "rotor.node"):
        assert not is_text(key), key
    assert never("llm.input_messages.0.message.contents.0.message_content.image.image.url")
    assert never("embedding.embeddings.0.embedding.vector")
    assert not never("llm.input_messages.0.message.content")


def test_text_leaves_only_with_content_and_each_value_is_cut_to_its_limit() -> None:
    attributes = {
        "openinference.span.kind": "LLM",
        "llm.model_name": "m" * 1000,
        "input.value": f"{SECRET} " + "x" * 20000,
        "output.value": "__REDACTED__",
        "llm.token_count.prompt": 12,
    }
    plain = to_message(span(attributes), scrub, content=False).attributes
    assert "input.value" not in plain and "output.value" not in plain
    assert len(plain["llm.model_name"]) == SPAN_ATTR_CHARS
    assert (plain["gen_ai.request.model"], plain["gen_ai.usage.input_tokens"]) == ("m" * SPAN_ATTR_CHARS, "12")

    told = to_message(span(attributes), scrub, content=True).attributes
    assert told["input.value"].startswith("[redacted] ") and len(told["input.value"]) == SPAN_CONTENT_CHARS
    assert "output.value" not in told, "OpenInference's placeholder is dropped, not sent"


def test_a_model_without_a_name_is_known_by_its_class() -> None:
    attributes = to_message(span({"openinference.span.kind": "LLM"}), scrub).attributes
    assert attributes["gen_ai.request.model"] == "ChatModel"


def test_rotor_s_keys_come_first_then_plain_ones_then_text() -> None:
    attributes = {f"llm.extra.{i:03}": "v" for i in range(SPAN_ATTRIBUTES)}
    attributes |= {"rotor.node": "a", "input.value": "text"}
    kept = to_message(span(attributes), scrub, content=True).attributes
    assert len(kept) == SPAN_ATTRIBUTES
    assert "rotor.node" in kept and "input.value" not in kept


def test_a_model_call_shows_its_last_messages_numbered_from_zero() -> None:
    total = SPAN_PROMPT_MESSAGES + 4
    attributes = {f"llm.input_messages.{i}.message.content": f"message {i}" for i in range(total)}
    kept = to_message(span(attributes), scrub, content=True).attributes
    assert kept["llm.input_messages.0.message.content"] == "message 4"
    assert kept[f"llm.input_messages.{SPAN_PROMPT_MESSAGES - 1}.message.content"] == f"message {total - 1}"
    assert f"llm.input_messages.{SPAN_PROMPT_MESSAGES}.message.content" not in kept
    assert kept["rotor.messages_omitted"] == "4"


def test_exceptions_show_without_text_and_an_interrupt_is_no_exception() -> None:
    retry = Event("exception", {"exception.type": "TimeoutError", "exception.stacktrace": "short"})
    failure = Event(
        "exception",
        {"exception.type": "ValueError", "exception.message": f"bad {SECRET}", "exception.stacktrace": "s" * 9000},
    )
    interrupt = Event("exception", {"exception.type": "GraphInterrupt", "exception.message": "waiting"})
    events = to_message(span({}, (retry,) * SPAN_EVENTS + (failure, interrupt)), scrub).events
    assert len(events) == SPAN_EVENTS
    assert events[-1].attributes["exception.message"] == "bad [redacted]"
    assert len(events[-1].attributes["exception.stacktrace"]) == SPAN_STACK_CHARS


def test_a_span_too_large_drops_its_longest_text_first_and_says_so() -> None:
    attributes = {f"llm.output_messages.{i}.message.content": str(i) * SPAN_CONTENT_CHARS for i in range(6)}
    attributes |= {"tool.name": "t", "rotor.node": "a"}
    message = to_message(span(attributes), scrub, content=True)
    kept = message.attributes
    assert message.ByteSize() <= SPAN_BYTES + 1024
    assert kept["rotor.content_cut"] == "true"
    assert (kept["tool.name"], kept["rotor.node"]) == ("t", "a")
    assert 0 < sum(k.startswith("llm.output_messages.") for k in kept) < 6
