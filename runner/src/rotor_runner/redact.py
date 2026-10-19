"""The runner cuts credentials from all it sends and logs (#41): every secret it holds, by its text, and every token
of a known shape, by the patterns in api/secret-patterns.json that the control plane applies again."""

from __future__ import annotations

import json
import logging
import re
from importlib import resources
from typing import Any

from rotor_runner.config import MIN_SECRET_CHARS

PATTERNS: dict[str, Any] = json.loads(resources.files("rotor_runner").joinpath("secret-patterns.json").read_text())
REDACTED: str = PATTERNS["redacted"]
_IN_TEXT = re.compile("(?:" + ")|(?:".join(PATTERNS["patterns"]) + ")")


def cut_known(text: str) -> str:
    """Text with every credential of a known shape replaced."""
    return _IN_TEXT.sub(REDACTED, text) if text else text


class Redactor:
    """Every secret the runner holds, and every token of a known shape, to cut from what leaves the sandbox."""

    def __init__(self) -> None:
        self._secrets: set[str] = set()

    def hold(self, secret: str) -> None:
        if len(secret) >= MIN_SECRET_CHARS:
            self._secrets.add(secret)

    def scrub(self, text: str) -> str:
        for secret in self._secrets:
            text = text.replace(secret, REDACTED)
        return cut_known(text)


class RedactingFormatter(logging.Formatter):
    """Formats a log record, traceback included, then cuts what the redactor would cut from an event."""

    def __init__(self, redactor: Redactor, fmt: str | None = None) -> None:
        super().__init__(fmt)
        self._redactor = redactor

    def format(self, record: logging.LogRecord) -> str:
        return self._redactor.scrub(super().format(record))
