"""What a tool call reports it wrote, for a trigger to tell its own writes from a person's (#127)."""

from rotor_runner.config import MAX_WRITE_SUBJECTS
from rotor_runner.events import subjects
from rotor_runner.mcp import called


def test_a_write_names_each_object_it_answered_with() -> None:
    assert subjects({"comment": {"id": "c-1", "body": {"type": "doc"}}}) == ["comment:c-1"]
    assert subjects({"requirement": {"id": 7, "key": "RTR-12", "revision": 3}}) == [
        "requirement:7",
        "requirement:RTR-12",
        "requirement:RTR-12@3",
    ]
    assert subjects({"requirement": {"key": "RTR-12", "revision": True}}) == ["requirement:RTR-12"]


def test_an_answer_of_another_shape_names_nothing() -> None:
    assert subjects([{"id": "x"}]) == []
    assert subjects({"text": "Done."}) == []
    assert subjects({"comment": {"id": ""}, "items": [{"id": "y"}]}) == []


def test_a_write_names_at_most_so_many() -> None:
    many = {f"m{i}": {"id": str(i)} for i in range(MAX_WRITE_SUBJECTS + 5)}
    assert len(subjects(many)) == MAX_WRITE_SUBJECTS


def test_the_event_says_whether_the_call_may_write_and_what_it_wrote() -> None:
    answer = {"comment": {"id": "c-1"}}
    write = called("armature", "add_comment", {"text": "Hi"}, answer, False, {"readOnlyHint": False})
    assert write["write"] is True
    assert write["subjects"] == ["comment:c-1"]
    assert write["ok"] is True

    read = called(
        "armature", "get_issue", {"issueKey": "CP-1"}, {"issue": {"key": "CP-1"}}, False, {"readOnlyHint": True}
    )
    assert read["write"] is False
    assert read["subjects"] == []

    failed = called("armature", "add_comment", {}, {"comment": {"id": "c-2"}}, True, None)
    assert failed["write"] is True
    assert failed["subjects"] == []
