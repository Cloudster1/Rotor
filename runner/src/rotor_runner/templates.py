"""Templates in a workflow: {{ inputs.x }} and {{ nodes.n.x }} are paths into the run's state, never code."""

from __future__ import annotations

import json
import re
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from rotor_runner.policy import Fence

TEMPLATE = re.compile(r"\{\{\s*((?:inputs|nodes)(?:\.[A-Za-z0-9_-]+)+)\s*\}\}")


def lookup(state: dict[str, Any], path: str) -> Any:
    """The value at a state path such as nodes.draft.title, or None where there is none."""
    value: Any = state
    for part in path.split("."):
        if not isinstance(value, dict) or part not in value:
            return None
        value = value[part]
    return value


def render(value: Any, state: dict[str, Any]) -> Any:
    """Fills {{ path }} templates in strings, nested anywhere; a string that is one template keeps the value's type."""
    if isinstance(value, dict):
        return {k: render(v, state) for k, v in value.items()}
    if isinstance(value, list):
        return [render(v, state) for v in value]
    if not isinstance(value, str):
        return value
    whole = TEMPLATE.fullmatch(value.strip())
    if whole:
        return lookup(state, whole.group(1))

    def text(match: re.Match[str]) -> str:
        found = lookup(state, match.group(1))
        return found if isinstance(found, str) else json.dumps(found, default=str)

    return TEMPLATE.sub(text, value)


def render_prompt(template: str, state: dict[str, Any], fence: Fence) -> str:
    """An agent's prompt with every value it quotes fenced as data (#40): only the author's own words instruct."""

    def text(match: re.Match[str]) -> str:
        found = lookup(state, match.group(1))
        quoted = found if isinstance(found, str) else json.dumps(found, default=str)
        return fence.wrap(match.group(1), quoted)

    return TEMPLATE.sub(text, template)
