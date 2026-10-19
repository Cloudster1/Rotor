"""The remote checkpointer: a run that waits at an approval resumes in another saver that shares only the store."""

from __future__ import annotations

from typing import Any

import pytest
from test_compile import FakeTools

from rotor_runner.checkpoint import CheckpointRefusedError, RemoteSaver
from rotor_runner.compile import RunContext, run_workflow
from rotor_runner.pb.rotor.runner.v1 import runner_pb2 as pb
from rotor_runner.spec import parse

pytestmark = pytest.mark.anyio

WORKFLOW = """apiVersion: rotor.dev/v1
kind: Workflow
metadata: {name: resume}
tools:
  t: {tool: exciter, scopes: [mcp], allowTools: [whoami]}
limits: {maxSteps: 10, timeout: 1m}
entry: ask
nodes:
  - {id: ask, type: tool, tool: t, call: whoami}
  - {id: confirm, type: approval, message: 'Go on?'}
  - {id: after, type: tool, tool: t, call: whoami}
edges:
  - {from: ask, to: confirm}
  - {from: confirm, to: after}
"""


class ControlPlane:
    """Answers checkpoint requests from memory, as the control plane does from Postgres; a limit refuses large ones."""

    def __init__(self, limit: int = 1 << 20) -> None:
        self.checkpoints: dict[str, pb.Checkpoint] = {}
        self.limit = limit

    async def request(self, message: pb.ConnectRequest, request_id: str) -> pb.ConnectResponse:
        kind = message.WhichOneof("message")
        if kind == "checkpoint_put":
            cp = message.checkpoint_put.checkpoint
            if len(cp.checkpoint.data) > self.limit:
                return pb.ConnectResponse(
                    checkpoint_put=pb.CheckpointPutResponse(request_id=request_id, error="too large")
                )
            self.checkpoints.setdefault(cp.checkpoint_id, cp)
            return pb.ConnectResponse(checkpoint_put=pb.CheckpointPutResponse(request_id=request_id))
        if kind == "checkpoint_writes":
            w = message.checkpoint_writes
            stored = self.checkpoints[w.checkpoint_id]
            known = {(p.task_id, p.idx) for p in stored.pending_writes}
            stored.pending_writes.extend(p for p in w.writes if (p.task_id, p.idx) not in known)
            return pb.ConnectResponse(checkpoint_writes=pb.CheckpointWritesResponse(request_id=request_id))
        if kind == "checkpoint_get":
            wanted = message.checkpoint_get.checkpoint_id or max(self.checkpoints, default="")
            found = self.checkpoints.get(wanted)
            answer = pb.CheckpointGetResponse(request_id=request_id, found=found is not None)
            if found is not None:
                answer.checkpoint.CopyFrom(found)
            return pb.ConnectResponse(checkpoint_get=answer)
        listed = sorted(self.checkpoints.values(), key=lambda c: c.checkpoint_id, reverse=True)
        return pb.ConnectResponse(checkpoint_list=pb.CheckpointListResponse(request_id=request_id, checkpoints=listed))


async def run(saver: RemoteSaver, tools: FakeTools, **kwargs: Any) -> Any:
    async def emit(_kind: str, _node: str, _payload: dict[str, Any]) -> None:
        pass

    ctx = RunContext(spec=parse(WORKFLOW), tools=tools, emit=emit)
    return await run_workflow(ctx, {}, thread_id="run-1", checkpointer=saver, **kwargs)


async def test_a_waiting_run_resumes_from_the_control_planes_checkpoint() -> None:
    plane, tools = ControlPlane(), FakeTools({"whoami": {"user": {"name": "bob"}}})
    first = await run(RemoteSaver(plane), tools)  # type: ignore[arg-type]
    assert first.status == "interrupted"
    assert [c[1] for c in tools.calls] == ["whoami"]
    assert plane.checkpoints

    # A fresh saver and graph, as in a new sandbox: only the control plane remembers the run.
    answer = {"resuming": True, "resume": {"approved": True}, "resume_id": first.interrupt["id"]}
    second = await run(RemoteSaver(plane), tools, **answer)  # type: ignore[arg-type]
    assert second.status == "succeeded", second
    assert second.output["confirm"] == {"approved": True, "comment": "", "decidedBy": ""}
    assert second.output["ask"] == {"user": {"name": "bob"}}
    assert [c[1] for c in tools.calls] == ["whoami", "whoami"], "the first call must not run again"


async def test_a_checkpoint_the_control_plane_refuses_ends_the_run() -> None:
    plane = ControlPlane(limit=10)
    with pytest.raises(CheckpointRefusedError):
        await run(RemoteSaver(plane), FakeTools({"whoami": {}}))  # type: ignore[arg-type]


async def test_the_saver_loads_only_safe_types() -> None:
    saver = RemoteSaver(ControlPlane())  # type: ignore[arg-type]
    kind, data = saver.serde.dumps_typed({"plain": [1, "two", {"three": 3.0}]})
    assert saver.serde.loads_typed((kind, data)) == {"plain": [1, "two", {"three": 3.0}]}
    assert saver.get_next_version(None, None).startswith("0" * 31 + "1.")
