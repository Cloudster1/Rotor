"""The runner judges the shared golden fixtures exactly as the control plane does (backend/.../spec_test.go)."""

from pathlib import Path

import pytest

from rotor_runner.spec import SCHEMA, SpecError, check, parse

ROOT = Path(__file__).resolve().parents[2]
FIXTURES = ROOT / "api" / "workflow-fixtures"
EXAMPLES = ROOT / "examples"


def expectations(source: str) -> list[tuple[str, str]]:
    out = []
    for line in source.splitlines():
        if line.startswith("# expect: "):
            path, code = line.removeprefix("# expect: ").split()
            out.append(("" if path == "-" else path, code))
    return out


@pytest.mark.parametrize("fixture", sorted((FIXTURES / "valid").glob("*.yaml")), ids=lambda p: p.name)
def test_a_valid_fixture_has_no_problems(fixture: Path) -> None:
    assert check(fixture.read_text()) == []
    assert parse(fixture.read_text()).entry


@pytest.mark.parametrize("example", sorted(EXAMPLES.glob("*.yaml")), ids=lambda p: p.name)
def test_an_example_has_no_problems(example: Path) -> None:
    assert check(example.read_text()) == []


@pytest.mark.parametrize("fixture", sorted((FIXTURES / "invalid").glob("*.yaml")), ids=lambda p: p.name)
def test_an_invalid_fixture_names_its_problem(fixture: Path) -> None:
    source = fixture.read_text()
    found = {(p.path, p.code) for p in check(source)}
    for expected in expectations(source):
        assert expected in found, f"want {expected}, got {found}"
    with pytest.raises(SpecError):
        parse(source)


def test_the_packaged_schema_is_the_one_in_api() -> None:
    import json

    assert json.loads((ROOT / "api" / "workflow.v1.schema.json").read_text()) == SCHEMA, "run make workflow-schema"


def test_a_spec_knows_what_follows_each_node() -> None:
    spec = parse((FIXTURES / "valid" / "triage-with-approval.yaml").read_text())
    assert spec.next("decide") == ["done", "confirm"]
    assert spec.next("confirm") == ["request"]
    assert spec.next("request") == []


def test_a_node_past_its_last_visit_leads_to_its_on_exhausted() -> None:
    spec = parse((FIXTURES / "valid" / "review-loop.yaml").read_text())
    assert spec.next("write") == ["review", "escalate"]
    assert spec.ways("write") == ["review"], "the graph's own edges leave the way past the last visit out"
