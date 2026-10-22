"""JUnit as the test tools write it (#138): each sample in tests/junit is what the tool itself wrote for a small
module with a passing, a failing and a skipped test."""

from __future__ import annotations

from pathlib import Path

import pytest

from rotor_runner import junit
from rotor_runner.config import MAX_TEST_CASES, MAX_TEST_FAILURES, TEST_LOG_TAIL_CHARS, TEST_MESSAGE_CHARS
from rotor_runner.testnode import summarize

SAMPLES = Path(__file__).parent / "junit"


def sample(name: str) -> list[junit.Case]:
    return junit.parse((SAMPLES / name).read_bytes())


def results(cases: list[junit.Case]) -> list[str]:
    return [c.result for c in cases]


def test_gotestsum_names_subtests_and_their_tags() -> None:
    cases = sample("gotestsum.xml")
    assert results(cases) == ["failed", "failed", "skipped", "passed"]
    sub = cases[0]
    assert sub.name == "example.com/sum > TestSubtracts/[verifies_CP-T2]_by_one"
    assert sub.verifies == ("CP-T2",) and sub.file == "example.com/sum"
    assert "2-1 = 1, want 2" in sub.message
    assert cases[3].verifies == (), "a name without brackets carries no tag"


def test_pytest_tags_by_property_and_parameter_and_counts_an_error_as_failing() -> None:
    cases = sample("pytest.xml")
    assert results(cases) == ["passed", "failed", "skipped", "failed"]
    assert cases[0].verifies == ("CP-T1",), "record_property('verifies', ...)"
    assert cases[1].name == "test_sum > test_subtracts[verifies CP-T2]" and cases[1].verifies == ("CP-T2",)
    assert cases[1].message.startswith("n = 1") and "assert (2 - 1) == 2" in cases[1].message
    assert "fixture 'missing_fixture' not found" in cases[3].message


def test_vitest_names_its_describe_blocks_and_tags_several_cases() -> None:
    cases = sample("vitest.xml")
    assert results(cases) == ["passed", "failed", "skipped"]
    assert cases[0].name == "sum.test.ts > sum > adds two numbers [verifies CP-T1]"
    assert cases[1].verifies == ("CP-T2", "CP-T3") and cases[1].file == "sum.test.ts"
    assert cases[1].message.startswith("AssertionError: expected 1 to be 2")


def test_node_writes_cases_outside_any_suite_and_names_the_file() -> None:
    cases = sample("node.xml")
    assert results(cases) == ["passed", "failed", "skipped"]
    assert cases[2].name == "signing in > skips the passkey for now"
    out = summarize(cases, "", Path("/w"))
    assert out["failures"][0]["file"] == "sample.test.mjs", "the work tree's path is left out"


@pytest.mark.parametrize(
    ("text", "keys"),
    [
        ("adds [verifies CP-T1]", ("CP-T1",)),
        ("[Verifies CP-T1,CP-T2] and [verifies CP-T1]", ("CP-T1", "CP-T2")),
        ("Test/[verifies_CP-T2]_by_one", ("CP-T2",)),
        ("[verifies: AUTH-12.3; AUTH-13]", ("AUTH-12.3", "AUTH-13")),
        ("verifies CP-T1", ()),
        ("[verifies]", ()),
        ("[verifies <script>]", ()),
    ],
)
def test_verifies_tags_name_test_case_keys(text: str, keys: tuple[str, ...]) -> None:
    assert junit.verifies(text) == keys


def test_a_tag_in_the_classname_counts_too() -> None:
    report = b'<testsuite><testcase classname="Login [verifies CP-T9]" name="works"/></testsuite>'
    assert junit.parse(report)[0].verifies == ("CP-T9",)


@pytest.mark.parametrize(
    "report",
    [
        b'<?xml version="1.0"?><!DOCTYPE lolz [<!ENTITY lol "lol"><!ENTITY lol2 "&lol;&lol;&lol;&lol;">]>'
        b'<testsuites><testcase name="&lol2;"/></testsuites>',
        b'<?xml version="1.0"?><!DOCTYPE t [<!ENTITY x SYSTEM "file:///etc/passwd">]><testsuites>&x;</testsuites>',
        b'<?xml version="1.0"?><!DOCTYPE t SYSTEM "http://example.invalid/t.dtd"><testsuites/>',
        b'<!DOCTYPE t [<!ENTITY % p SYSTEM "http://example.invalid/p">%p;]><testsuites/>',
    ],
)
def test_a_report_declaring_a_dtd_or_an_entity_is_refused(report: bytes) -> None:
    with pytest.raises(junit.ReportError, match="DTD or an entity"):
        junit.parse(report)


@pytest.mark.parametrize(
    ("report", "says"),
    [
        (b"<html><body/></html>", "not <testsuites>"),
        (b"<testsuites><testcase>", "not well-formed"),
        (b"", "well-formed"),
    ],
)
def test_what_is_not_junit_is_refused_saying_so(report: bytes, says: str) -> None:
    with pytest.raises(junit.ReportError, match=says):
        junit.parse(report)


def test_the_output_holds_every_count_and_caps_what_it_names() -> None:
    many = [junit.Case(f"case {i}", "passed", ()) for i in range(MAX_TEST_CASES + 500)]
    many += [junit.Case(f"tagged {i}", "passed", (f"CP-T{i}",)) for i in range(10)]
    many += [
        junit.Case(f"broken {i}", "failed", (), "x" * (TEST_MESSAGE_CHARS * 2)) for i in range(MAX_TEST_FAILURES + 5)
    ]
    out = summarize(many, "y" * (TEST_LOG_TAIL_CHARS + 100) + "the end", Path("/workspace/01-test"))
    assert (out["passed"], out["failed"], out["skipped"]) == (MAX_TEST_CASES + 510, MAX_TEST_FAILURES + 5, 0)
    assert len(out["cases"]) == MAX_TEST_CASES and len(out["failures"]) == MAX_TEST_FAILURES
    named = {c["name"] for c in out["cases"]}
    assert {f"tagged {i}" for i in range(10)} <= named, "a case that verifies one is kept before the rest"
    assert {f"broken {i}" for i in range(MAX_TEST_FAILURES)} <= named
    assert len(out["logTail"]) == TEST_LOG_TAIL_CHARS and out["logTail"].endswith("the end")


def test_a_long_failure_message_keeps_its_start_and_end() -> None:
    body = "AssertionError: first line\n" + "frame\n" * 2000 + "at sum_test.go:14"
    report = (
        f'<testsuite><testcase name="t"><failure message="the sum is wrong">{body}</failure></testcase></testsuite>'
    )
    message = junit.parse(report.encode())[0].message
    assert len(message) <= TEST_MESSAGE_CHARS
    assert message.startswith("the sum is wrong\nAssertionError: first line") and message.endswith("at sum_test.go:14")
