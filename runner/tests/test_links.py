"""Links an agent writes are limited to the run's tools' hosts (#40)."""

from __future__ import annotations

import pytest

from rotor_runner.config import LINK_REMOVED
from rotor_runner.links import LinkLimiter

LIMITER = LinkLimiter(["exciter.example", "Wiki.Example"])


@pytest.mark.parametrize(
    "text",
    [
        "see https://exciter.example/p/R-1.",
        "see http://wiki.example:8080/page?x=1",
        "a [link](https://exciter.example/r/1) in markdown",
        "plain text with no link at all, and an email a@b.example",
    ],
)
def test_links_to_the_tools_stay(text: str) -> None:
    assert LIMITER.limit(text) == (text, [])


@pytest.mark.parametrize(
    ("text", "host"),
    [
        ("see https://evil.example/x?d=secret now", "evil.example"),
        ("![pixel](https://evil.example/p.png?d=secret)", "evil.example"),
        ("HTTPS://EVIL.EXAMPLE/X", "evil.example"),
        ("go to //evil.example/x", "evil.example"),
        ("https://exciter.example@evil.example/x", "evil.example"),
        ("https://exciter.example.evil.example/x", "exciter.example.evil.example"),
        ("www.evil.example/x", "www.evil.example"),
        # Hosts match exactly, so even a tool's www. is another host.
        ("www.exciter.example/evil", "www.exciter.example"),
        ("(https://evil.example)", "evil.example"),
    ],
)
def test_links_to_other_hosts_are_cut(text: str, host: str) -> None:
    out, removed = LIMITER.limit(text)
    assert removed == [host]
    assert LINK_REMOVED in out
    assert "evil" not in out


def test_trailing_punctuation_stays_with_the_sentence() -> None:
    assert LIMITER.limit("read https://evil.example/x.") == (f"read {LINK_REMOVED}.", ["evil.example"])


def test_values_are_limited_wherever_they_nest() -> None:
    value = {"body": ["ok", {"note": "see https://evil.example/a"}], "n": 1, "link": "https://exciter.example/r"}
    out, removed = LIMITER.limit_value(value)
    assert out == {"body": ["ok", {"note": f"see {LINK_REMOVED}"}], "n": 1, "link": "https://exciter.example/r"}
    assert removed == ["evil.example"]


def test_no_hosts_means_no_links() -> None:
    assert LinkLimiter([]).limit("https://exciter.example/r") == (LINK_REMOVED, ["exciter.example"])
