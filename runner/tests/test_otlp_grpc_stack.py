"""A LangChain app's spans reach Rotor over OTLP/gRPC (#262) through the stock OpenTelemetry gRPC exporter, set up with
the three variables the deployment guide tells an app to set and nothing else."""

from __future__ import annotations

import os

import pytest
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor, SpanExportResult
from opentelemetry.trace import Link
from test_otlp_stack import KEYCLOAK, Kept, realm_token

# Where the api takes OTLP over gRPC on the stack's network.
GRPC = os.environ.get("ROTOR_TEST_OTLP_GRPC_URL", "")


@pytest.mark.stack
def test_the_stock_grpc_exporter_sends_an_apps_spans_and_rotor_takes_them(monkeypatch: pytest.MonkeyPatch) -> None:
    if not (GRPC and KEYCLOAK):
        pytest.skip("run by make test-runner, inside the stack's network")
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_ENDPOINT", GRPC)
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_HEADERS", f"authorization=Bearer%20{realm_token()}")

    kept = Kept()
    provider = TracerProvider(resource=Resource.create({"service.name": "langchain-app"}))
    provider.add_span_processor(SimpleSpanProcessor(kept))
    tracer = provider.get_tracer("test")
    with tracer.start_as_current_span("LangGraph", attributes={"openinference.span.kind": "CHAIN"}) as first:
        pass
    # A span of a batch names the span it handles, as a queue's consumer does.
    with tracer.start_as_current_span("handle", links=[Link(first.get_span_context())]):
        pass
    provider.shutdown()
    assert len(kept.spans) == 2 and len(kept.spans[1].links) == 1

    assert OTLPSpanExporter().export(kept.spans) == SpanExportResult.SUCCESS

    # Without the token the same exporter is refused, so the success above was the token's.
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
    assert OTLPSpanExporter().export(kept.spans) == SpanExportResult.FAILURE
