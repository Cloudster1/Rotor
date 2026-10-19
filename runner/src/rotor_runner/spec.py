"""A workflow as the runner reads it: the control plane's schema and sense checks, reporting paths alone.
YAML 1.2 as the control plane reads it, so `on` is a key and not a boolean."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from importlib import resources
from typing import Any, cast

from jsonschema import Draft202012Validator
from ruamel.yaml import YAML
from ruamel.yaml.error import YAMLError

from rotor_runner.config import (
    APPROVAL_MAX_EXPIRY_S,
    CODE_MAX_MEMORY,
    CODE_MAX_TIMEOUT_S,
    DURATION_UNITS,
    MAX_SPEC_BYTES,
    MEBIBYTE,
    RESERVED_FORM_FIELDS,
    REVIEW_DECIDING_CALLS,
)

TEMPLATE_REF = re.compile(r"\{\{\s*([A-Za-z0-9_.-]+)\s*\}\}")
REVIEW_DECIDING = re.compile(REVIEW_DECIDING_CALLS)

SCHEMA: dict[str, Any] = json.loads(resources.files("rotor_runner").joinpath("workflow.v1.schema.json").read_text())
_VALIDATOR = Draft202012Validator(SCHEMA)


def _load(source: str | bytes) -> Any:
    """Plain dicts, lists and scalars, as JSON would hold them."""
    return YAML(typ="safe", pure=True).load(source)


@dataclass(frozen=True)
class Problem:
    """One thing wrong with a workflow, at a JSON pointer, with the code the control plane uses."""

    path: str
    code: str
    message: str


@dataclass
class Spec:
    """A workflow that passed every check, as plain data."""

    document: dict[str, Any]
    nodes: dict[str, dict[str, Any]] = field(default_factory=dict)

    @property
    def tools(self) -> dict[str, dict[str, Any]]:
        return cast(dict[str, dict[str, Any]], self.document.get("tools", {}))

    @property
    def limits(self) -> dict[str, Any]:
        return cast(dict[str, Any], self.document["limits"])

    @property
    def entry(self) -> str:
        return cast(str, self.document["entry"])

    def next(self, node_id: str) -> list[str]:
        """The nodes that may follow a node: its edges, or a router's cases and default."""
        return successors(self.document, self.nodes[node_id])


class SpecError(ValueError):
    """A workflow the runner cannot run, with every problem found."""

    def __init__(self, problems: list[Problem]) -> None:
        super().__init__("; ".join(f"{p.path or '/'}: {p.message}" for p in problems))
        self.problems = problems


def parse(source: str | bytes) -> Spec:
    """Read and check a workflow, raising SpecError with every problem."""
    problems = check(source)
    if problems:
        raise SpecError(problems)
    document = cast(dict[str, Any], _load(source))
    return Spec(document=document, nodes={n["id"]: n for n in document["nodes"]})


def check(source: str | bytes) -> list[Problem]:
    """Every problem with a workflow: the YAML, the schema, then its own sense."""
    raw = source.encode() if isinstance(source, str) else source
    if len(raw) > MAX_SPEC_BYTES:
        return [Problem("", "too_large", f"The workflow is {len(raw)} bytes; keep it under {MAX_SPEC_BYTES}.")]
    try:
        document = _load(raw)
    except YAMLError as err:
        return [Problem("", "yaml", f"The YAML cannot be read: {err}.")]
    schema_problems = [
        Problem(
            _pointer(list(error.absolute_path)),
            "schema",
            f"At {_pointer(list(error.absolute_path)) or 'the top level'}: {error.message}.",
        )
        for error in _leaves(list(_VALIDATOR.iter_errors(document)))
    ]
    if schema_problems:
        return schema_problems
    return check_sense(cast(dict[str, Any], document))


def _leaves(errors: list[Any]) -> list[Any]:
    """The innermost reasons, beneath the schema's grouping into allOf, anyOf and if/then."""
    out: list[Any] = []
    for error in errors:
        if error.context:
            out.extend(_leaves(list(error.context)))
        else:
            out.append(error)
    return out


def _pointer(parts: list[Any]) -> str:
    if not parts:
        return ""
    return "/" + "/".join(str(p).replace("~", "~0").replace("/", "~1") for p in parts)


def successors(document: dict[str, Any], node: dict[str, Any]) -> list[str]:
    if node["type"] == "router":
        cases = cast(dict[str, str], node.get("cases", {}))
        out = [cases[k] for k in sorted(cases)]
        if node.get("default"):
            out.append(cast(str, node["default"]))
        return out
    return [e["to"] for e in document.get("edges", []) if e["from"] == node["id"]]


def _state_node(path: str) -> str:
    parts = path.split(".")
    return parts[1] if len(parts) >= 2 and parts[0] == "nodes" else ""


def _argument_refs(value: Any, path: str) -> list[tuple[str, str]]:
    if isinstance(value, dict):
        return [ref for k in sorted(value) for ref in _argument_refs(value[k], f"{path}/{_escape(k)}")]
    if isinstance(value, list):
        return [ref for i, item in enumerate(value) for ref in _argument_refs(item, f"{path}/{i}")]
    if isinstance(value, str):
        return [(path, node) for m in TEMPLATE_REF.finditer(value) if (node := _state_node(m.group(1)))]
    return []


def _escape(key: str) -> str:
    return key.replace("~", "~0").replace("/", "~1")


def _code_limits(node: dict[str, Any], base: str) -> list[Problem]:
    """A code node asking for more time or memory than any node gets, as Go's codeLimits."""
    out: list[Problem] = []
    timeout = str(node.get("timeout") or "")
    if timeout and float(timeout[:-1]) * DURATION_UNITS[timeout[-1]] > CODE_MAX_TIMEOUT_S:
        out.append(Problem(f"{base}/timeout", "code_limits", f"A code node may run for 5m at most, not {timeout}."))
    memory = str(node.get("memory") or "")
    if memory and int(memory[:-2]) * {"Mi": MEBIBYTE, "Gi": 1024 * MEBIBYTE}[memory[-2:]] > CODE_MAX_MEMORY:
        out.append(
            Problem(f"{base}/memory", "code_limits", f"A code node may use 1Gi of memory at most, not {memory}.")
        )
    return out


def check_sense(document: dict[str, Any]) -> list[Problem]:
    """What the schema cannot find: names pointing nowhere and nodes nothing leads to, as Go's checkSense."""
    out: list[Problem] = []
    nodes: list[dict[str, Any]] = document["nodes"]
    tools: dict[str, dict[str, Any]] = document.get("tools", {})
    index: dict[str, int] = {}
    for i, node in enumerate(nodes):
        if node["id"] in index:
            out.append(Problem(f"/nodes/{i}/id", "duplicate_id", f"Another node is already called {node['id']}."))
            continue
        index[node["id"]] = i
    entry = document["entry"]
    if entry not in index:
        out.append(Problem("/entry", "unknown_node", f"The entry {entry} is not a node of this workflow."))
    for i, edge in enumerate(document.get("edges", [])):
        if edge["from"] not in index:
            out.append(
                Problem(f"/edges/{i}/from", "unknown_node", f"The edge starts at {edge['from']}, which is not a node.")
            )
        elif nodes[index[edge["from"]]]["type"] == "router":
            out.append(Problem(f"/edges/{i}/from", "router_edges", f"The router {edge['from']} takes no edges."))
        if edge["to"] not in index:
            out.append(
                Problem(f"/edges/{i}/to", "unknown_node", f"The edge leads to {edge['to']}, which is not a node.")
            )
    for i, node in enumerate(nodes):
        base = f"/nodes/{i}"
        if node["type"] == "router":
            for value in sorted(node.get("cases", {})):
                if node["cases"][value] not in index:
                    out.append(
                        Problem(f"{base}/cases/{_escape(value)}", "unknown_node", f"The case {value} leads nowhere.")
                    )
            if node.get("default") and node["default"] not in index:
                out.append(Problem(f"{base}/default", "unknown_node", f"The default {node['default']} is not a node."))
            ref = _state_node(node["on"])
            if ref and ref not in index:
                out.append(Problem(f"{base}/on", "unknown_node", f"The router reads {ref}, which is not a node."))
        elif node["type"] == "tool":
            binding = tools.get(node["tool"])
            if binding is None:
                out.append(
                    Problem(f"{base}/tool", "unknown_binding", f"{node['tool']} is not one of the workflow's tools.")
                )
            elif node["call"] not in binding["allowTools"]:
                out.append(
                    Problem(
                        f"{base}/call",
                        "call_not_allowed",
                        f"{node['call']} is not in the allowTools of {node['tool']}.",
                    )
                )
            for path, ref in _argument_refs(node.get("arguments", {}), f"{base}/arguments"):
                if ref not in index:
                    out.append(Problem(path, "unknown_node", f"The argument reads {ref}, which is not a node."))
        elif node["type"] == "approval":
            out.extend(_approval_sense(node, base, index))
        elif node["type"] in ("agent", "code"):
            if node["type"] == "code":
                out.extend(_code_limits(node, base))
            for j, name in enumerate(node.get("tools", [])):
                if name not in tools:
                    out.append(
                        Problem(f"{base}/tools/{j}", "unknown_binding", f"{name} is not one of the workflow's tools.")
                    )
    out.extend(_review_deciders(tools))
    if entry in index:
        reached = {entry}
        queue = [entry]
        while queue:
            current = queue.pop(0)
            for to in successors(document, nodes[index[current]]) if current in index else []:
                if to not in reached:
                    reached.add(to)
                    queue.append(to)
        for i, node in enumerate(nodes):
            if node["id"] not in reached and index.get(node["id"]) == i:
                out.append(
                    Problem(f"/nodes/{i}", "unreachable", f"Nothing leads to {node['id']} from the entry {entry}.")
                )
    return out


def _approval_sense(node: dict[str, Any], base: str, index: dict[str, int]) -> list[Problem]:
    """An approval's expiry within the longest wait and a form an approver can fill, as Go's approvalSense."""
    out: list[Problem] = []
    expires = str(node.get("expiresIn") or "")
    if expires and float(expires[:-1]) * DURATION_UNITS[expires[-1]] > APPROVAL_MAX_EXPIRY_S:
        message = f"An approval may wait 168h at most, not {expires}."
        out.append(Problem(f"{base}/expiresIn", "approval_expiry", message))
    form = node.get("form")
    if form is None:
        return out
    properties = form_fields(form["schema"])
    if properties is None:
        out.append(Problem(f"{base}/form/schema", "approval_form", "The form's schema must be an object with fields."))
        return out
    for name in sorted(properties):
        if name in RESERVED_FORM_FIELDS:
            out.append(
                Problem(f"{base}/form/schema/properties/{_escape(name)}", "approval_form", f"{name} is reserved.")
            )
    values = form.get("values") or {}
    for name in sorted(values):
        if name not in properties:
            out.append(Problem(f"{base}/form/values/{_escape(name)}", "approval_form", f"{name} is not a field."))
    for path, ref in _argument_refs(values, f"{base}/form/values"):
        if ref not in index:
            out.append(Problem(path, "unknown_node", f"The value reads {ref}, which is not a node."))
    return out


def form_fields(schema: Any) -> dict[str, Any] | None:
    """A form schema's properties, or None when it is no object schema with fields that checks."""
    if not isinstance(schema, dict) or schema.get("type") != "object":
        return None
    properties = schema.get("properties")
    if not isinstance(properties, dict) or not properties:
        return None
    try:
        Draft202012Validator.check_schema(schema)
    except Exception:
        return None
    return properties


def _review_deciders(tools: dict[str, dict[str, Any]]) -> list[Problem]:
    """A binding that allows a call deciding a review is refused: a person decides a review in the tool itself."""
    out: list[Problem] = []
    for name in sorted(tools):
        for j, call in enumerate(tools[name].get("allowTools", [])):
            if REVIEW_DECIDING.match(call):
                path = f"/tools/{_escape(name)}/allowTools/{j}"
                out.append(Problem(path, "decides_review", f"{call} decides a review."))
    return out
