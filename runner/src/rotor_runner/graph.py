"""The smallest graph the runner builds; compiled workflows (#16) take the same shape."""

from typing import Annotated, TypedDict

from langchain_core.language_models import BaseChatModel
from langchain_core.language_models.fake_chat_models import GenericFakeChatModel
from langchain_core.messages import AIMessage, AnyMessage, HumanMessage
from langgraph.graph import END, START, StateGraph
from langgraph.graph.message import add_messages
from langgraph.graph.state import CompiledStateGraph

SELF_CHECK_ANSWER = "ready"


class State(TypedDict):
    messages: Annotated[list[AnyMessage], add_messages]


def build(model: BaseChatModel) -> CompiledStateGraph[State]:
    """One node that hands the conversation to the model and appends its answer."""

    def call_model(state: State) -> dict[str, list[AnyMessage]]:
        return {"messages": [model.invoke(state["messages"])]}

    graph = StateGraph(State)
    graph.add_node("model", call_model)
    graph.add_edge(START, "model")
    graph.add_edge("model", END)
    return graph.compile()


def self_check() -> str:
    """Runs the graph with a fake model and loads every dependency a sandbox needs, offline."""
    import grpc  # noqa: F401
    import langchain_mcp_adapters.client  # noqa: F401

    model = GenericFakeChatModel(messages=iter([AIMessage(content=SELF_CHECK_ANSWER)]))
    result = build(model).invoke({"messages": [HumanMessage(content="Are you ready?")]})
    return str(result["messages"][-1].content)
