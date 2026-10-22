"""Bounded loops (#134): state.visits counts each node's runs, so the count is checkpointed with the run, and a way
into a node with no visit left goes to its onExhausted or ends the run as failed with loop_exhausted."""

from __future__ import annotations

import functools
from collections.abc import Callable
from typing import Any

from rotor_runner.limits import failed
from rotor_runner.spec import Spec


def visits(state: Any) -> dict[str, int]:
    """How often each node has run; a run checkpointed before #134 has no count yet."""
    found = state.get("visits") if isinstance(state, dict) else None
    return found if isinstance(found, dict) else {}


def counted(run: Callable[..., Any], node_id: str) -> Callable[..., Any]:
    """A node that adds its visit to what it writes; wraps keeps the signature LangGraph reads to pass its config."""

    @functools.wraps(run)
    async def count(state: Any, *args: Any, **kwargs: Any) -> dict[str, Any]:
        update = await run(state, *args, **kwargs)
        return {**update, "visits": {node_id: visits(state).get(node_id, 0) + 1}}

    return count


def arrive(spec: Spec, target: str, state: Any) -> str:
    """Where a way into target leads: target itself, or past its last visit its onExhausted, as far as that goes."""
    counts, passed = visits(state), [target]
    while True:
        node = spec.nodes[target]
        limit = int(node.get("maxVisits") or 0)
        if not limit or counts.get(target, 0) < limit:
            return target
        after = node.get("onExhausted")
        if not after:
            raise failed("loop_exhausted", f"{target} ran {limit} times, its maxVisits, and names no onExhausted")
        if after in passed:
            raise failed("loop_exhausted", f"{target} ran {limit} times, and so did every onExhausted after it")
        target = str(after)
        passed.append(target)


def landings(spec: Spec, target: str) -> list[str]:
    """Every node a way into target may lead to, for the graph's path map."""
    out = [target]
    while True:
        node = spec.nodes[out[-1]]
        after = node.get("onExhausted")
        if not node.get("maxVisits") or not after or after in out:
            return out
        out.append(str(after))


def counts_anywhere(spec: Spec, targets: list[str]) -> bool:
    """Whether a way into any of targets may end elsewhere, so it must be chosen as the run goes."""
    return any(spec.nodes[t].get("maxVisits") for t in targets)
