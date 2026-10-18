"""The smallest graph the runner builds; compiled workflows (#16) take the same shape."""

import asyncio
from typing import Annotated, Any, TypedDict

from langchain_core.language_models import BaseChatModel
from langchain_core.language_models.fake_chat_models import GenericFakeChatModel
from langchain_core.messages import AIMessage, AnyMessage, HumanMessage
from langchain_core.tools import BaseTool
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


CODE_CHECK = """apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: self-check}
limits: {maxSteps: 3, timeout: 30s}
entry: check
nodes:
  - id: check
    type: code
    output: {schema: {type: object, required: [answer]}}
    source: |
      def run(state, ctx):
          return {"answer": state["inputs"]["answer"]}
"""


def self_check() -> str:
    """Runs a graph with a fake model and a workflow with a code node, offline, as a sandbox would."""
    import grpc  # noqa: F401
    import langchain_mcp_adapters.client  # noqa: F401

    model = GenericFakeChatModel(messages=iter([AIMessage(content=SELF_CHECK_ANSWER)]))
    result = build(model).invoke({"messages": [HumanMessage(content="Are you ready?")]})
    answer = str(result["messages"][-1].content)
    code_answer = asyncio.run(_code_check())
    return answer if code_answer == answer else f"code nodes answered {code_answer!r}"


async def _code_check() -> str:
    from rotor_runner.codenode import CodeNodeRunner
    from rotor_runner.compile import RunContext, run_workflow
    from rotor_runner.events import discard
    from rotor_runner.spec import parse

    ctx = RunContext(spec=parse(CODE_CHECK), tools=_NoTools(), emit=discard, code=CodeNodeRunner())
    outcome = await run_workflow(ctx, {"answer": SELF_CHECK_ANSWER}, thread_id="self-check")
    return str(outcome.output.get("check", {}).get("answer", f"{outcome.status} {outcome.reason}"))


class _NoTools:
    """The self-check calls no tool."""

    async def call(self, binding: str, name: str, arguments: dict[str, Any], node: str) -> Any:
        raise RuntimeError("the self-check calls no tool")

    async def langchain_tools(self, bindings: list[str], node: str) -> list[BaseTool]:
        return []
