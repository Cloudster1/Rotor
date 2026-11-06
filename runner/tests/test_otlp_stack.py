"""A LangChain app's spans reach Rotor's OTLP endpoint (#243) through the stock OpenTelemetry exporter, set up with the
two variables the changelog tells an app to set and nothing else."""

from __future__ import annotations

import os
from collections.abc import Sequence

import httpx
import pytest
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import ReadableSpan, TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor, SpanExporter, SpanExportResult

# The stack's api and Keycloak on its network, and the suite's service account in the development realm (#168).
API, KEYCLOAK = os.environ.get("ROTOR_TEST_API_URL", ""), os.environ.get("ROTOR_TEST_KEYCLOAK_URL", "")
REALM, CLIENT, CLIENT_SECRET = "dynamo", "dynamo-connect", "dynamo-connect-dev-secret"


class Kept(SpanExporter):
    """Keeps the spans an app ended, so the test exports them itself and reads what the exporter answered."""

    def __init__(self) -> None:
        self.spans: list[ReadableSpan] = []

    def export(self, spans: Sequence[ReadableSpan]) -> SpanExportResult:
        self.spans.extend(spans)
        return SpanExportResult.SUCCESS


def realm_token() -> str:
    answer = httpx.post(
        f"{KEYCLOAK}/realms/{REALM}/protocol/openid-connect/token",
        data={"grant_type": "client_credentials", "client_id": CLIENT, "client_secret": CLIENT_SECRET},
        timeout=10,
    )
    assert answer.status_code == httpx.codes.OK, answer.text
    return str(answer.json()["access_token"])


@pytest.mark.stack
def test_the_stock_exporter_sends_an_apps_spans_and_rotor_takes_them(monkeypatch: pytest.MonkeyPatch) -> None:
    if not (API and KEYCLOAK):
        pytest.skip("run by make test-runner, inside the stack's network")
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_ENDPOINT", f"{API}/api/v1/otlp")
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_HEADERS", f"Authorization=Bearer%20{realm_token()}")

    kept = Kept()
    provider = TracerProvider(resource=Resource.create({"service.name": "langchain-app"}))
    provider.add_span_processor(SimpleSpanProcessor(kept))
    tracer = provider.get_tracer("test")
    with (
        tracer.start_as_current_span("LangGraph", attributes={"openinference.span.kind": "CHAIN"}),
        tracer.start_as_current_span("ChatOpenAI", attributes={"openinference.span.kind": "LLM"}),
    ):
        pass
    provider.shutdown()
    assert len(kept.spans) == 2

    exporter = OTLPSpanExporter()
    assert exporter._endpoint == f"{API}/api/v1/otlp/v1/traces", "the SDK adds /v1/traces to the endpoint"
    assert exporter.export(kept.spans) == SpanExportResult.SUCCESS

    # Without the token the same exporter is refused, so the success above was the token's.
    monkeypatch.setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
    assert OTLPSpanExporter().export(kept.spans) == SpanExportResult.FAILURE
