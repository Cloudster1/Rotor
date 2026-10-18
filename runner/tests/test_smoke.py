from langchain_core.language_models.fake_chat_models import GenericFakeChatModel
from langchain_core.messages import AIMessage, HumanMessage

from rotor_runner.__main__ import EXIT_USAGE, main
from rotor_runner.graph import SELF_CHECK_ANSWER, build, self_check


def test_the_graph_appends_the_models_answer_to_the_conversation() -> None:
    model = GenericFakeChatModel(messages=iter([AIMessage(content="Two requirements are ambiguous.")]))
    result = build(model).invoke({"messages": [HumanMessage(content="Review the specification.")]})
    contents = [m.content for m in result["messages"]]
    assert contents == ["Review the specification.", "Two requirements are ambiguous."]


def test_the_self_check_answers_ready() -> None:
    assert self_check() == SELF_CHECK_ANSWER


def test_the_entry_point_runs_the_self_check_and_refuses_anything_else() -> None:
    assert main(["--self-check"]) == 0
    assert main([]) == EXIT_USAGE
