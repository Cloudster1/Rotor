"""Links an agent writes may point only at the run's tools (#40), so text it read cannot have it send data to a host
of the text's choosing, whether in its answer or in what it writes to a tool."""

from __future__ import annotations

import re
from collections.abc import Iterable
from typing import Any
from urllib.parse import urlsplit

from rotor_runner.config import LINK_REMOVED

# A link with a scheme, a protocol-relative one after a space or an opening bracket, or one starting www.; markdown
# links and images are links with a scheme inside brackets.
_LINK = re.compile(r"(?:\bhttps?://|(?<![^\s(\[<\"'])//|\bwww\.)[^\s<>\"'`)\]]+", re.IGNORECASE)
# Punctuation that ends a sentence rather than the link.
_TRAILING = ".,;:!?"


def _host(link: str) -> str:
    """The host a link reaches, lowercased and ASCII, or empty when it has none Rotor can read."""
    if link.startswith("//"):
        link = "https:" + link
    elif link.lower().startswith("www."):
        link = "https://" + link
    try:
        host = urlsplit(link).hostname or ""
        return host.encode("idna").decode("ascii").lower()
    except (ValueError, UnicodeError):
        return ""


class LinkLimiter:
    """Keeps links to exactly the given hosts and replaces every other one."""

    def __init__(self, hosts: Iterable[str]) -> None:
        self.hosts = frozenset(h for h in (_host("//" + h) for h in hosts) if h)

    def limit(self, text: str) -> tuple[str, list[str]]:
        """Text with each link to another host replaced, and the hosts it named, in order."""
        removed: list[str] = []

        def keep_or_cut(match: re.Match[str]) -> str:
            link = match.group(0)
            core = link.rstrip(_TRAILING)
            host = _host(core)
            if host and host in self.hosts:
                return link
            removed.append(host or "?")
            return LINK_REMOVED + link[len(core) :]

        return _LINK.sub(keep_or_cut, text), removed

    def limit_value(self, value: Any) -> tuple[Any, list[str]]:
        """A value with every text in it limited, nested anywhere, and the hosts removed."""
        if isinstance(value, str):
            return self.limit(value)
        removed: list[str] = []
        if isinstance(value, dict):
            out: dict[Any, Any] = {}
            for k, v in value.items():
                out[k], gone = self.limit_value(v)
                removed += gone
            return out, removed
        if isinstance(value, list):
            items = []
            for v in value:
                item, gone = self.limit_value(v)
                items.append(item)
                removed += gone
            return items, removed
        return value, removed
