"""What the runner cuts from its events, output and log lines (#41): held secrets and tokens of a known shape."""

from __future__ import annotations

import json
import logging
from pathlib import Path

import pytest

from rotor_runner.redact import PATTERNS, REDACTED, RedactingFormatter, Redactor, cut_known

ROOT = Path(__file__).resolve().parents[2]


@pytest.mark.parametrize(
    "secret",
    [
        "ghp_" + "0123456789abcdefABCDEF0123456789",
        "github_pat_" + "11ABCDEFG0123456789_abcdefghijk",
        "glpat-" + "0123456789abcdefABCD",
        "xoxb-" + "1234567890-abcdefghij",
        "AKIAIOSFODNN7EXAMPLE",
        "sk-ant-api03-xyz",
        "sk_live_" + "0123456789abcdefgh",
        "exciter_oat_9z",
        "rotor_oat_0123456789",
        "hvs.CAESIJ0123456789",
        "eyJhbGciOiJSUzI1NiJ9.e30.c2lnbmF0dXJl",
        "-----BEGIN PRIVATE KEY-----\nMIIEow\n-----END PRIVATE KEY-----",
    ],
)
def test_a_token_of_a_known_shape_is_cut(secret: str) -> None:
    assert cut_known(f"before {secret} after") == f"before {REDACTED} after"


def test_a_password_in_a_url_is_cut_and_the_host_kept() -> None:
    assert "hunter2" not in cut_known("clone https://alice:hunter2@git.example/repo")
    assert "git.example/repo" in cut_known("clone https://alice:hunter2@git.example/repo")


@pytest.mark.parametrize("text", ["a task-force met", "the sk-learn library", "eyJ starts base64 JSON", "tokens: 1200"])
def test_ordinary_text_stays(text: str) -> None:
    assert cut_known(text) == text


def test_cutting_a_token_from_json_keeps_it_json() -> None:
    redactor = Redactor()
    text = json.dumps(
        {"log": "pushed with ghp_" + "0123456789abcdefABCDEF0123456789", "url": "https://a:b@h/x", "n": 1}
    )
    assert json.loads(redactor.scrub(text))["n"] == 1


def test_a_log_line_and_its_traceback_lose_every_secret() -> None:
    redactor = Redactor()
    redactor.hold("provider-key-0123456789")
    formatter = RedactingFormatter(redactor, "%(message)s")
    try:
        raise RuntimeError("the provider refused provider-key-0123456789 and ghp_" + "0123456789abcdefABCDEF0123456789")
    except RuntimeError:
        import sys

        record = logging.LogRecord("t", logging.ERROR, __file__, 1, "call failed", None, sys.exc_info())
    line = formatter.format(record)
    assert "provider-key-0123456789" not in line
    assert "ghp_" + "0123456789" not in line
    assert "call failed" in line


def test_the_packaged_patterns_are_the_ones_in_api() -> None:
    assert json.loads((ROOT / "api" / "secret-patterns.json").read_text()) == PATTERNS, "run make secret-patterns"
