"""The one place Rotor imports OpenInference's private LangChain handler (#255)."""

from __future__ import annotations

# OpenInference has no public way to build this handler for one tracer provider: LangChainInstrumentor installs one in
# every callback manager of the process, and two runs would share spans. Checked against
# openinference-instrumentation-langchain 0.1.79 (and 0.1.80); tests/test_langchain_tracer.py fails if it moves.
from openinference.instrumentation.langchain._tracer import OpenInferenceTracer
from opentelemetry import trace

__all__ = ["OpenInferenceTracer", "handler"]


def handler(tracer: trace.Tracer) -> OpenInferenceTracer:
    """A callback handler that writes spans with this tracer alone, nested in the current context."""
    return OpenInferenceTracer(tracer, separate_trace_from_runtime_context=False)
