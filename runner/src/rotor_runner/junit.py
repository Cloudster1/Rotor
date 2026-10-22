"""JUnit reports as test tools write them (#138): gotestsum, pytest, vitest and node's own runner. The report comes
from the repository's code, so the parser refuses any DTD, which no JUnit file needs and every entity attack does."""

from __future__ import annotations

import re
import xml.etree.ElementTree as ET
from dataclasses import dataclass
from typing import Any
from xml.parsers import expat

from rotor_runner.config import TEST_MESSAGE_CHARS, TEST_NAME_CHARS

PASSED, FAILED, SKIPPED = "passed", "failed", "skipped"

# A tag such as [verifies CP-T1] or [verifies CP-T1, CP-T2]; Go's subtests write the spaces as underscores.
_TAG = re.compile(r"\[verifies[\s_:]+([^\]]+)\]", re.IGNORECASE)
_KEY = re.compile(r"^[A-Za-z][A-Za-z0-9.-]*$")
_SEPARATORS = re.compile(r"[\s_,;]+")
# What a case is called: its class or file, then its own name, as vitest writes a describe block.
_NAME_JOIN = " > "
_CUT = "\n[...]\n"


class ReportError(Exception):
    """A report that is not JUnit, or that declares what a JUnit file never needs; the message is a sentence."""


@dataclass(frozen=True)
class Case:
    """One test case as a report tells it."""

    name: str
    result: str
    verifies: tuple[str, ...]
    message: str = ""
    file: str = ""


def verifies(*texts: str) -> tuple[str, ...]:
    """The test case keys [verifies KEY] tags in the texts name, each once, in order."""
    out: list[str] = []
    for text in texts:
        for tag in _TAG.finditer(text):
            out.extend(k for k in _SEPARATORS.split(tag.group(1)) if _KEY.match(k) and k not in out)
    return tuple(out)


def _refuse(*_args: Any) -> None:
    raise ReportError("The report declares a DTD or an entity, which a JUnit report never needs; it is not read.")


def parse_xml(data: bytes) -> ET.Element:
    """The document, read by expat with every DTD refused, so no entity is ever expanded or fetched."""
    parser = expat.ParserCreate()
    parser.SetParamEntityParsing(expat.XML_PARAM_ENTITY_PARSING_NEVER)
    parser.StartDoctypeDeclHandler = _refuse
    parser.EntityDeclHandler = _refuse
    parser.UnparsedEntityDeclHandler = _refuse
    parser.ExternalEntityRefHandler = _refuse  # type: ignore[assignment]
    builder = ET.TreeBuilder()
    parser.StartElementHandler = builder.start
    parser.EndElementHandler = builder.end
    parser.CharacterDataHandler = builder.data
    try:
        parser.Parse(data, True)
    except expat.ExpatError as err:
        raise ReportError(
            f"The report is not well-formed XML: {expat.ErrorString(err.code)}, line {err.lineno}."
        ) from None
    return builder.close()


def cut(text: str, limit: int) -> str:
    """Text at most limit characters long, its start and end kept when it is longer."""
    if len(text) <= limit:
        return text
    half = (limit - len(_CUT)) // 2
    return text[:half] + _CUT + text[-half:]


def _message(element: ET.Element) -> str:
    said, body = (element.get("message") or "").strip(), (element.text or "").strip()
    text = body if said in body else f"{said}\n{body}".strip()
    return cut(text, TEST_MESSAGE_CHARS)


def _case(element: ET.Element, suite_file: str) -> Case:
    own, group = element.get("name") or "", element.get("classname") or ""
    name = cut(f"{group}{_NAME_JOIN}{own}" if group and own else own or group, TEST_NAME_CHARS)
    tagged = [p.get("value") or "" for p in element.iter("property") if (p.get("name") or "").lower() == "verifies"]
    keys = verifies(own, group, *(f"[verifies {v}]" for v in tagged))
    where = element.get("file") or suite_file or group
    bad = element.find("failure")
    if bad is None:
        bad = element.find("error")
    if bad is not None:
        return Case(name, FAILED, keys, _message(bad), where)
    if element.find("skipped") is not None:
        return Case(name, SKIPPED, keys, file=where)
    return Case(name, PASSED, keys, file=where)


def parse(data: bytes) -> list[Case]:
    """Every test case of a report, wherever its suites nest them; ReportError for anything but JUnit."""
    root = parse_xml(data)
    if root.tag not in ("testsuites", "testsuite"):
        raise ReportError(f"The report's root is <{root.tag}>, not <testsuites> or <testsuite>; write JUnit XML.")
    out: list[Case] = []

    def walk(element: ET.Element, suite_file: str) -> None:
        for child in element:
            if child.tag == "testcase":
                out.append(_case(child, suite_file))
            elif child.tag == "testsuite":
                walk(child, child.get("file") or suite_file)

    walk(root, root.get("file") or "")
    return out
