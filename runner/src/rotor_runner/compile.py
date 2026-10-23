"""A workflow spec compiled into a LangGraph graph, and one run of it streamed into events.
Node outputs live under state.nodes; tokens never do, they stay with the ToolGateway."""

from __future__ import annotations

import asyncio
import copy
import functools
import json
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
from rotor_runner.events import EventSink, summary, top_node
from rotor_runner.limits import RunEnded, TokenBudget, TokenBudgetError, failed
from rotor_runner.links import LinkLimiter
from rotor_runner.mcp import ToolFailedError, ToolGateway, ToolRefusedError
from rotor_runner.policy import Fence, policy_text
from rotor_runner.preview import preview
from rotor_runner.spec import Spec
from rotor_runner.telemetry import RunTracing
from rotor_runner.templates import lookup, render, render_prompt
from rotor_runner.untrusted import UntrustedResults, report_removed
from rotor_runner.usage import UsageReporter

OUTPUT_TOOL = "submit_output"


def _merge(left: dict[str, Any], right: dict[str, Any]) -> dict[str, Any]:
    return {**left, **right}


class State(TypedDict):
    inputs: dict[str, Any]
    nodes: Annotated[dict[str, Any], _merge]


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
    # The models of the agents that name their own, by node id (#133); the others use model.
    agent_models: dict[str, Callable[[], BaseChatModel]] = field(default_factory=dict)
    code: CodeRunner | None = None
    tracing: RunTracing = field(default_factory=RunTracing)
    # The public hosts of the run's tools, the only ones an agent's links may name (#40).
    link_hosts: frozenset[str] = frozenset()
    fence: Fence = field(default_factory=Fence)
    links: LinkLimiter = field(init=False)

    def __post_init__(self) -> None:
        self.links = LinkLimiter(self.link_hosts)


@dataclass
class Outcome:
    """How a run ended in this sandbox: succeeded, failed, cancelled, or interrupted waiting for an answer."""

    status: str
    reason: str = ""
    output: dict[str, Any] = field(default_factory=dict)
    interrupt: dict[str, Any] | None = None


def check_output(node: dict[str, Any], value: Any) -> Any:
    """A node's output, refused when it does not match the schema the node declares."""
    schema = (node.get("output") or {}).get("schema")
    if schema is not None:
        errors = sorted(Draft202012Validator(schema).iter_errors(value), key=lambda e: list(e.path))
        if errors:
            raise failed("output_schema", f"{node['id']} returned output its schema refuses: {errors[0].message}")
    return value


def _agent_model(node: dict[str, Any], ctx: RunContext) -> Callable[[], BaseChatModel]:
    """The agent's own model, or the workflow's when it names none; never the workflow's in place of its own."""
    model = ctx.agent_models.get(node["id"]) if "model" in node else ctx.model
    if model is None:
        raise failed("model_missing", f"{node['id']} needs a language model, and none is set up for this run")
    return model


def _agent(node: dict[str, Any], ctx: RunContext) -> Callable[..., Any]:
    async def run(state: State, config: RunnableConfig) -> dict[str, Any]:
        model = _agent_model(node, ctx)
        tools = await ctx.tools.langchain_tools(list(node.get("tools", [])), node["id"])
        schema = (node.get("output") or {}).get("schema")
        response_format: ToolStrategy[dict[str, Any]] | None = (
            ToolStrategy({**copy.deepcopy(schema), "title": OUTPUT_TOOL}) if schema else None
        )
        agent = create_agent(
            model(),
            tools,
            system_prompt=policy_text(ctx.link_hosts, str(node.get("role") or "")),
            middleware=[UntrustedResults(ctx.fence, ctx.links, ctx.emit, node["id"])],
            response_format=response_format,
        )
        prompt = render_prompt(str(node["prompt"]), cast(dict[str, Any], state), ctx.fence)
        # The agent's own steps cap its loop of model and tool calls; without them it has the workflow's.
        steps = int(node.get("maxSteps") or ctx.spec.limits["maxSteps"])
        result = await agent.ainvoke({"messages": [HumanMessage(prompt)]}, {**config, "recursion_limit": steps})
        if schema:
            value = result.get("structured_response")
        else:
            last = next((m for m in reversed(result["messages"]) if isinstance(m, AIMessage)), None)
            value = {"text": last.text if last else ""}
        value, removed = ctx.links.limit_value(value)
        await report_removed(ctx.emit, node["id"], "output", removed)
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


def _approval(node: dict[str, Any], spec: Spec) -> Callable[..., Any]:
    """Waits for a person, showing the proposed values and the writes ahead; it outputs the approved values."""

    async def run(state: State) -> dict[str, Any]:
        current = cast(dict[str, Any], state)
        form = node.get("form") or {}
        values = render(form.get("values") or {}, current)
        ahead = {**current, "nodes": {**current.get("nodes", {}), node["id"]: values}}
        answer = interrupt(
            {
                "node": node["id"],
                "message": render(node["message"], current),
                "values": values,
                "preview": preview(spec, node["id"], ahead),
            }
        )
        if not isinstance(answer, dict):
            answer = {}
        if answer.get("approved") is False:
            raise RunEnded("cancelled", "rejected", f"{node['id']} was not approved")
        sent = answer.get("values")
        approved: dict[str, Any] = sent if isinstance(sent, dict) else values
        output = {
            **approved,
            "approved": True,
            "comment": str(answer.get("comment") or ""),
            "decidedBy": str(answer.get("decidedBy") or ""),
        }
        return {"nodes": {node["id"]: output}}

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


def _traced(run: Callable[..., Any], node: dict[str, Any], tracing: RunTracing) -> Callable[..., Any]:
    """A node under a span of its own (#37); wraps keeps its signature, which LangGraph reads to pass its config."""
    if not tracing.on:
        return run

    @functools.wraps(run)
    async def traced(*args: Any, **kwargs: Any) -> Any:
        with tracing.span(f"node {node['id']}", {"rotor.node": node["id"], "rotor.node.type": node["type"]}):
            return await run(*args, **kwargs)

    return traced


def compile_workflow(ctx: RunContext, checkpointer: BaseCheckpointSaver[Any] | None = None) -> Any:
    """The spec as a compiled graph; a checkpointer lets an approval wait and the run resume."""
    graph = StateGraph(State)
    makers: dict[str, Callable[[dict[str, Any]], Callable[..., Any]]] = {
        "agent": lambda n: _agent(n, ctx),
        "tool": lambda n: _tool(n, ctx),
        "code": lambda n: _code(n, ctx),
        "approval": lambda n: _approval(n, ctx.spec),
        "router": lambda _n: _pass,
    }
    for node in ctx.spec.nodes.values():
        graph.add_node(node["id"], _traced(makers[node["type"]](node), node, ctx.tracing))
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


async def run_workflow(ctx: RunContext, inputs: dict[str, Any], *, thread_id: str, **kwargs: Any) -> Outcome:
    """Runs the workflow, under the run's span when it is traced (#37); _run_workflow says the rest."""
    with ctx.tracing.root("run", {"rotor.run": thread_id}):
        return await _run_workflow(ctx, inputs, thread_id=thread_id, **kwargs)


async def _run_workflow(
    ctx: RunContext,
    inputs: dict[str, Any],
    *,
    thread_id: str,
    checkpointer: BaseCheckpointSaver[Any] | None = None,
    resume: Any = None,
    resuming: bool = False,
    resume_id: str = "",
) -> Outcome:
    """Runs the workflow, or resumes it with the answer to the interrupt resume_id names, emitting its events."""
    limits = ctx.spec.limits
    graph = compile_workflow(ctx, checkpointer)
    config: RunnableConfig = {"configurable": {"thread_id": thread_id}, "recursion_limit": int(limits["maxSteps"])}
    callbacks: list[Any] = [UsageReporter(ctx.emit)]
    if limits.get("maxTokens"):
        callbacks.append(TokenBudget(int(limits["maxTokens"])))
    if ctx.tracing.on:
        callbacks.extend(ctx.tracing.callbacks())
    config["callbacks"] = callbacks
    start: Any = {"inputs": inputs, "nodes": {}}
    snapshot = await graph.aget_state(config) if checkpointer is not None else None
    waiting_at = {i.id for task in snapshot.tasks for i in task.interrupts} if snapshot else set()
    if resuming and resume_id in waiting_at:
        # Keyed by the interrupt, an answer never answers a later approval the run reaches.
        start = Command(resume={resume_id: resume})
    elif resuming and not resume_id:
        start = Command(resume=resume)
    elif snapshot is not None and snapshot.next:
        # A sandbox that replaced a lost one carries on from the run's last checkpoint.
        start = None
    buffers: dict[str, str] = {}

    async def flush(node: str, done: bool = True) -> None:
        head, rest = whole_words(buffers.pop(node, ""), done)
        if rest:
            buffers[node] = rest
        if head:
            await ctx.emit("llm.output", node, {"text": ctx.links.limit(head)[0]})

    try:
        async with asyncio.timeout(_seconds(str(limits["timeout"]))):
            async for mode, chunk in graph.astream(start, config, stream_mode=["tasks", "updates", "messages"]):
                if mode == "messages":
                    message, metadata = chunk
                    if isinstance(message, AIMessageChunk) and message.text:
                        node = top_node(metadata)
                        buffers[node] = buffers.get(node, "") + message.text
                        if len(buffers[node]) >= LLM_FLUSH_CHARS:
                            await flush(node, done=False)
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
        value = waiting[0].value if isinstance(waiting[0].value, dict) else {"message": str(waiting[0].value)}
        return Outcome("interrupted", "", interrupt={**value, "id": waiting[0].id})
    return Outcome("succeeded", "", cast(dict[str, Any], snapshot.values.get("nodes", {})))


def whole_words(text: str, done: bool) -> tuple[str, str]:
    """Streamed text split into what may go now and what waits: mid-stream only whole words go, so a link is never
    cut in two before it is limited (#40)."""
    cut = len(text) if done else max(text.rfind(" "), text.rfind("\n")) + 1
    return text[:cut], text[cut:]


def _seconds(duration: str) -> float:
    return float(duration[:-1]) * DURATION_UNITS[duration[-1]]
