"""Parallel branches (#135): a join waits as LangGraph's deferred node does, until no branch has a step left, and
approvals that parallel branches reach together are reported, and answered, together."""

from __future__ import annotations

from typing import Any

from rotor_runner.loops import landings
from rotor_runner.spec import Spec


def deferred(spec: Spec) -> set[str]:
    """The nodes that wait for every branch: each join, and the onExhausted nodes a way into it may end at instead."""
    out: set[str] = set()
    for node in spec.nodes.values():
        if node.get("join") == "all":
            out.update(landings(spec, node["id"]))
    return out


def reported(interrupts: list[Any]) -> dict[str, Any]:
    """What a waiting run tells the control plane: the first interrupt as a lone one always was, and, when parallel
    branches wait together, every one of them under waiting."""
    each = [{**(i.value if isinstance(i.value, dict) else {"message": str(i.value)}), "id": i.id} for i in interrupts]
    return {**each[0], "waiting": each} if len(each) > 1 else each[0]


def answering(answers: dict[str, Any], waiting_at: set[str]) -> dict[str, Any]:
    """The answers to interrupts the run still waits at; an answer to any other never answers a later approval."""
    return {i: a for i, a in answers.items() if i in waiting_at}
