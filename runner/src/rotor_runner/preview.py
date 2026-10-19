"""What an approval shows before the run goes on: the writes the nodes after it may make, until another approval.
Only the runner holds the state, so it renders them; the control plane only caps and redacts what it is sent."""

from __future__ import annotations

from typing import Any

from rotor_runner.config import APPROVAL_PREVIEW_ENTRIES
from rotor_runner.spec import Spec
from rotor_runner.templates import render


def preview(spec: Spec, approval: str, state: dict[str, Any]) -> list[dict[str, Any]]:
    """The writes reachable from an approval along every branch, stopping at the next approval or the end."""
    out: list[dict[str, Any]] = []
    seen = {approval}
    queue = list(spec.next(approval))
    while queue and len(out) < APPROVAL_PREVIEW_ENTRIES:
        node_id = queue.pop(0)
        if node_id in seen:
            continue
        seen.add(node_id)
        node = spec.nodes[node_id]
        if node["type"] == "approval":
            continue
        out.extend(_writes(spec, node, state))
        queue.extend(spec.next(node_id))
    return out[:APPROVAL_PREVIEW_ENTRIES]


def _writes(spec: Spec, node: dict[str, Any], state: dict[str, Any]) -> list[dict[str, Any]]:
    """A tool node's call with its arguments; an agent's or code node's write bindings with the calls they allow."""
    tools = spec.tools
    if node["type"] == "tool":
        binding = tools[node["tool"]]
        if binding.get("access") != "write":
            return []
        arguments = render(node.get("arguments", {}), state)
        entry = {"node": node["id"], "binding": node["tool"], "tool": binding["tool"], "call": node["call"]}
        return [{**entry, "arguments": arguments}]
    if node["type"] in ("agent", "code"):
        return [
            {
                "node": node["id"],
                "binding": name,
                "tool": tools[name]["tool"],
                "calls": list(tools[name]["allowTools"]),
                "arguments": None,
            }
            for name in node.get("tools", [])
            if tools[name].get("access") == "write"
        ]
    return []
