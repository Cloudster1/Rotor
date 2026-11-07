"""Rotor leans on a private OpenInference module (#255); an update that moves it must fail here, with the reason."""

import inspect

from rotor_runner import langchain_tracer

HINT = (
    "openinference-instrumentation-langchain moved or changed OpenInferenceTracer, which Rotor builds per run "
    "(rotor_runner/langchain_tracer.py). Look for a public per-tracer-provider handler (#255) or adapt the module."
)


def test_the_private_handler_still_takes_what_the_runner_passes() -> None:
    parameters = inspect.signature(langchain_tracer.OpenInferenceTracer).parameters
    assert {"tracer", "separate_trace_from_runtime_context"} <= set(parameters), HINT
    assert callable(getattr(langchain_tracer.OpenInferenceTracer, "get_span", None)), HINT
