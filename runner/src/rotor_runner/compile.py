"""A workflow spec compiled into a LangGraph graph, and one run of it streamed into events.
Node outputs live under state.nodes; tokens never do, they stay with the ToolGateway."""

from __future__ import annotations

import asyncio
import copy
import json
import re
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Annotated, Any, Protocol, TypedDict, cast

from jsonschema import Draft202012Validator
from langchain.agents import create_agent
from langchain.agents.structured_output import ToolStrategy
from langchain_core.language_models import BaseChatModel
from langchain_core.messages import AIMessage, AIMessageChunk, HumanMessage
from langchain_core.runnables import RunnableConfig
from langgraph.checkpoint.base import BaseCheckpointSaver
from langgraph.checkpoint.memory import InMemorySaver
from langgraph.errors import GraphRecursionError
from langgraph.graph import END, START, StateGraph
from langgraph.types import Command, interrupt

from rotor_runner.config import DURATION_UNITS, LLM_FLUSH_CHARS
from rotor_runner.events import EventSink, summary
from rotor_runner.limits import TokenBudget, TokenBudgetError
from rotor_runner.mcp import ToolFailedError, ToolGateway, ToolRefusedError
from rotor_runner.spec import Spec

TEMPLATE = re.compile(r"\{\{\s*((?:inputs|nodes)(?:\.[A-Za-z0-9_-]+)+)\s*\}\}")
OUTPUT_TOOL = "submit_output"


def _merge(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return {**left, **right}


class State(TypedDict):
    inputs: dict[str, Any]
    nodes: Annotated[dict[str, Any], _merge]


class RunEnded(Exception):  # noqa: N818 - it is how a run ends, not always an error
    """A run that stops before its graph does, with the outcome and reason the control plane records."""

    def __init__(self, status: str, reason: str, message: str) -> None:
        super().__init__(message)
        self.status, self.reason = status, reason


def failed(reason: str, message: str) -> RunEnded:
    return RunEnded("failed", reason, message)


class CodeRunner(Protocol):
    """Runs a code node; rotor_runner.codenode is the real one."""

    async def run(self, node: dict[str, Any], state: dict[str, Any], ctx: RunContext) -> Any: ...


@dataclass
class RunContext:
    """Everything a run's nodes reach beyond the state."""

    spec: Spec
    tools: ToolGateway
    emit: EventSink
    model: Callable[[], BaseChatModel] | None = None
    code: CodeRunner | None = None


@dataclass
class Outcome:
    """How a run ended in this sandbox: succeeded, failed, cancelled, or interrupted waiting for an answer."""

    status: str
    reason: str = ""
    output: dict[str, Any] = field(default_factory=dict)
    interrupt: dict[str, Any] | None = None


def lookup(state: dict[str, Any], path: str) -> Any:
    """The value at a state path such as nodes.draft.title, or None where there is none."""
    value: Any = state
    for part in path.split("."):
        if not isinstance(value, dict) or part not in value:
            return None
        value = value[part]
    return value


def render(value: Any, state: dict[str, Any]) -> Any:
    """Fills {{ path }} templates in strings, nested anywhere; a string that is one template keeps the value's type."""
    if isinstance(value, dict):
        return {k: render(v, state) for k, v in value.items()}
    if isinstance(value, list):
        return [render(v, state) for v in value]
    if not isinstance(value, str):
        return value
    whole = TEMPLATE.fullmatch(value.strip())
    if whole:
        return lookup(state, whole.group(1))

    def text(match: re.Match[str]) -> str:
        found = lookup(state, match.group(1))
        return found if isinstance(found, str) else json.dumps(found, default=str)

    return TEMPLATE.sub(text, value)


def check_output(node: dict[str, Any], value: Any) -> Any:
    """A node's output, refused when it does not match the schema the node declares."""
    schema = (node.get("output") or {}).get("schema")
    if schema is not None:
        errors = sorted(Draft202012Validator(schema).iter_errors(value), key=lambda e: list(e.path))
        if errors:
            raise failed("output_schema", f"{node['id']} returned output its schema refuses: {errors[0].message}")
    return value


def _agent(node: dict[str, Any], ctx: RunContext) -> Callable[..., Any]:
    async def run(state: State, config: RunnableConfig) -> dict[str, Any]:
        if ctx.model is None:
            raise failed("model_missing", f"{node['id']} needs a language model, and none is set up for this run")
        tools = await ctx.tools.langchain_tools(list(node.get("tools", [])), node["id"])
        schema = (node.get("output") or {}).get("schema")
        response_format: ToolStrategy[dict[str, Any]] | None = (
            ToolStrategy({**copy.deepcopy(schema), "title": OUTPUT_TOOL}) if schema else None
        )
        agent = create_agent(ctx.model(), tools, response_format=response_format)
        prompt = render(node["prompt"], cast(dict[str, Any], state))
        result = await agent.ainvoke({"messages": [HumanMessage(str(prompt))]}, config)
        if schema:
            value = result.get("structured_response")
        else:
            last = next((m for m in reversed(result["messages"]) if isinstance(m, AIMessage)), None)
            value = {"text": last.text if last else ""}
        return {"nodes": {node["id"]: check_output(node, value)}}

    return run


def _tool(node: dict[str, Any], ctx: RunContext) -> Callable[..., Any]:
    async def run(state: State) -> dict[str, Any]:
        arguments = render(node.get("arguments", {}), cast(dict[str, Any], state))
        try:
            value = await ctx.tools.call(node["tool"], node["call"], arguments, node["id"])
        except ToolRefusedError as err:
            raise failed("tool_refused", str(err)) from err
        except ToolFailedError as err:
            raise failed("tool_error", str(err)) from err
        return {"nodes": {node["id"]: value}}

    return run


def _code(node: dict[str, Any], ctx: RunContext) -> Callable[..., Any]:
    async def run(state: State) -> dict[str, Any]:
        if ctx.code is None:
            raise failed("code_unavailable", f"{node['id']} is a code node, and this runner cannot run code")
        value = await ctx.code.run(node, cast(dict[str, Any], state), ctx)
        return {"nodes": {node["id"]: check_output(node, value)}}

    return run


def _approval(node: dict[str, Any]) -> Callable[..., Any]:
    async def run(state: State) -> dict[str, Any]:
        answer = interrupt({"node": node["id"], "message": render(node["message"], cast(dict[str, Any], state))})
        if isinstance(answer, dict) and answer.get("approved") is False:
            raise RunEnded("cancelled", "rejected", f"{node['id']} was not approved")
        return {"nodes": {node["id"]: answer}}

    return run


def _route(node: dict[str, Any]) -> Callable[[State], str]:
    def choose(state: State) -> str:
        value = lookup(cast(dict[str, Any], state), node["on"])
        key = value if isinstance(value, str) else json.dumps(value)
        cases = cast(dict[str, str], node["cases"])
        if key in cases:
            return cases[key]
        if node.get("default"):
            return cast(str, node["default"])
        raise failed("no_route", f"{node['id']} has no case for {key} and no default")

    return choose


async def _pass(_state: State) -> dict[str, Any]:
    return {}


def compile_workflow(ctx: RunContext, checkpointer: BaseCheckpointSaver[Any] | None = None) -> Any:
    """The spec as a compiled graph; a checkpointer lets an approval wait and the run resume."""
    graph = StateGraph(State)
    makers: dict[str, Callable[[dict[str, Any]], Callable[..., Any]]] = {
        "agent": lambda n: _agent(n, ctx),
        "tool": lambda n: _tool(n, ctx),
        "code": lambda n: _code(n, ctx),
        "approval": _approval,
        "router": lambda _n: _pass,
    }
    for node in ctx.spec.nodes.values():
        graph.add_node(node["id"], makers[node["type"]](node))
    graph.add_edge(START, ctx.spec.entry)
    for node in ctx.spec.nodes.values():
        targets = ctx.spec.next(node["id"])
        if node["type"] == "router":
            graph.add_conditional_edges(node["id"], _route(node), {t: t for t in targets})
        elif not targets:
            graph.add_edge(node["id"], END)
        for target in targets if node["type"] != "router" else []:
            graph.add_edge(node["id"], target)
    return graph.compile(checkpointer=checkpointer or InMemorySaver())


def _top_node(metadata: dict[str, Any]) -> str:
    namespace = str(metadata.get("langgraph_checkpoint_ns") or metadata.get("checkpoint_ns") or "")
    top = namespace.split("|")[0].split(":")[0]
    return top or str(metadata.get("langgraph_node", ""))


async def run_workflow(
    ctx: RunContext,
    inputs: dict[str, Any],
    *,
    thread_id: str,
    checkpointer: BaseCheckpointSaver[Any] | None = None,
    resume: Any = None,
    resuming: bool = False,
) -> Outcome:
    """Runs the workflow, or resumes it with the answer to the interrupt it waits at, emitting its events."""
    limits = ctx.spec.limits
    graph = compile_workflow(ctx, checkpointer)
    config: RunnableConfig = {"configurable": {"thread_id": thread_id}, "recursion_limit": int(limits["maxSteps"])}
    if limits.get("maxTokens"):
        config["callbacks"] = [TokenBudget(int(limits["maxTokens"]))]
    start: Any = Command(resume=resume) if resuming else {"inputs": inputs, "nodes": {}}
    buffers: dict[str, str] = {}

    async def flush(node: str) -> None:
        if buffers.get(node):
            await ctx.emit("llm.output", node, {"text": buffers.pop(node)})

    try:
        async with asyncio.timeout(_seconds(str(limits["timeout"]))):
            async for mode, chunk in graph.astream(start, config, stream_mode=["tasks", "updates", "messages"]):
                if mode == "messages":
                    message, metadata = chunk
                    if isinstance(message, AIMessageChunk) and message.text:
                        node = _top_node(metadata)
                        buffers[node] = buffers.get(node, "") + message.text
                        if len(buffers[node]) >= LLM_FLUSH_CHARS:
                            await flush(node)
                elif mode == "tasks" and not str(chunk.get("name", "")).startswith("__"):
                    node = str(chunk["name"])
                    if "result" in chunk or "error" in chunk:
                        await flush(node)
                        error = chunk.get("error")
                        await ctx.emit(
                            "node.finished",
                            node,
                            {
                                "ok": error is None,
                                "error": str(error) if error else None,
                                "outputSummary": summary(chunk.get("result")),
                            },
                        )
                    else:
                        await ctx.emit("node.started", node, {"type": ctx.spec.nodes.get(node, {}).get("type", "")})
                elif mode == "updates" and "__interrupt__" in chunk:
                    for pending in chunk["__interrupt__"]:
                        value = pending.value if isinstance(pending.value, dict) else {"message": str(pending.value)}
                        await ctx.emit("interrupt", str(value.get("node", "")), {"message": value.get("message", "")})
    except GraphRecursionError:
        return Outcome("failed", "max_steps")
    except TimeoutError:
        return Outcome("failed", "timeout")
    except TokenBudgetError:
        return Outcome("failed", "max_tokens")
    except RunEnded as ended:
        return Outcome(ended.status, ended.reason, {"message": str(ended)})
    snapshot = await graph.aget_state(config)
    waiting = [i for task in snapshot.tasks for i in task.interrupts]
    if waiting:
        return Outcome("interrupted", "", interrupt=cast(dict[str, Any], waiting[0].value))
    return Outcome("succeeded", "", cast(dict[str, Any], snapshot.values.get("nodes", {})))


def _seconds(duration: str) -> float:
    return float(duration[:-1]) * DURATION_UNITS[duration[-1]]
