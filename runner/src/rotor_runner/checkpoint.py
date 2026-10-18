"""A LangGraph checkpointer that keeps nothing in the sandbox: every checkpoint goes to the control plane over the
runner's stream, so a run resumes in a fresh sandbox. It loads only the serializer's safe types."""

from __future__ import annotations

import random
from collections.abc import AsyncIterator, Sequence
from typing import TYPE_CHECKING, Any

from langchain_core.runnables import RunnableConfig
from langgraph.checkpoint.base import (
    WRITES_IDX_MAP,
    BaseCheckpointSaver,
    ChannelVersions,
    Checkpoint,
    CheckpointMetadata,
    CheckpointTuple,
    get_checkpoint_id,
    get_checkpoint_metadata,
    writes_sort_key,
)
from langgraph.checkpoint.serde.jsonplus import JsonPlusSerializer

from rotor_runner.pb.rotor.runner.v1 import runner_pb2 as pb

if TYPE_CHECKING:
    from rotor_runner.control import Channel


class CheckpointRefusedError(RuntimeError):
    """The control plane would not keep a checkpoint, such as one larger than a run may keep."""


class RemoteSaver(BaseCheckpointSaver[str]):
    """Stores and loads checkpoints through the control plane; the run's id is its thread."""

    def __init__(self, channel: Channel) -> None:
        super().__init__(serde=JsonPlusSerializer(allowed_msgpack_modules=None))
        self._channel = channel

    def _tuple(self, thread_id: str, cp: pb.Checkpoint) -> CheckpointTuple:
        namespace = cp.checkpoint_ns
        writes = sorted(cp.pending_writes, key=lambda w: writes_sort_key(w.task_path, w.task_id, w.idx))
        parent = None
        if cp.parent_checkpoint_id:
            parent = RunnableConfig(
                configurable={
                    "thread_id": thread_id,
                    "checkpoint_ns": namespace,
                    "checkpoint_id": cp.parent_checkpoint_id,
                }
            )
        return CheckpointTuple(
            config=RunnableConfig(
                configurable={"thread_id": thread_id, "checkpoint_ns": namespace, "checkpoint_id": cp.checkpoint_id}
            ),
            checkpoint=self.serde.loads_typed((cp.checkpoint.type, cp.checkpoint.data)),
            metadata=self.serde.loads_typed((cp.metadata.type, cp.metadata.data)),
            parent_config=parent,
            pending_writes=[
                (w.task_id, w.channel, self.serde.loads_typed((w.value.type, w.value.data))) for w in writes
            ],
        )

    async def aget_tuple(self, config: RunnableConfig) -> CheckpointTuple | None:
        thread_id, namespace = _thread(config)
        request_id = _request_id()
        get = pb.CheckpointGetRequest(
            request_id=request_id, checkpoint_ns=namespace, checkpoint_id=get_checkpoint_id(config) or ""
        )
        answer = (await self._channel.request(pb.ConnectRequest(checkpoint_get=get), request_id)).checkpoint_get
        if answer.error:
            raise CheckpointRefusedError(answer.error)
        return self._tuple(thread_id, answer.checkpoint) if answer.found else None

    async def alist(
        self,
        config: RunnableConfig | None,
        *,
        filter: dict[str, Any] | None = None,
        before: RunnableConfig | None = None,
        limit: int | None = None,
    ) -> AsyncIterator[CheckpointTuple]:
        if config is None:
            return
        thread_id, namespace = _thread(config)
        request_id = _request_id()
        listing = pb.CheckpointListRequest(
            request_id=request_id,
            checkpoint_ns=namespace,
            limit=limit or 0,
            before_checkpoint_id=(get_checkpoint_id(before) if before else None) or "",
        )
        answer = (await self._channel.request(pb.ConnectRequest(checkpoint_list=listing), request_id)).checkpoint_list
        if answer.error:
            raise CheckpointRefusedError(answer.error)
        for cp in answer.checkpoints:
            found = self._tuple(thread_id, cp)
            if filter and any(found.metadata.get(k) != v for k, v in filter.items()):
                continue
            yield found

    async def aput(
        self,
        config: RunnableConfig,
        checkpoint: Checkpoint,
        metadata: CheckpointMetadata,
        new_versions: ChannelVersions,
    ) -> RunnableConfig:
        thread_id, namespace = _thread(config)
        value_type, value = self.serde.dumps_typed(checkpoint)
        meta_type, meta = self.serde.dumps_typed(get_checkpoint_metadata(config, metadata))
        request_id = _request_id()
        put = pb.CheckpointPutRequest(
            request_id=request_id,
            checkpoint=pb.Checkpoint(
                checkpoint_ns=namespace,
                checkpoint_id=checkpoint["id"],
                parent_checkpoint_id=get_checkpoint_id(config) or "",
                checkpoint=pb.TypedValue(type=value_type, data=value),
                metadata=pb.TypedValue(type=meta_type, data=meta),
            ),
        )
        answer = (await self._channel.request(pb.ConnectRequest(checkpoint_put=put), request_id)).checkpoint_put
        if answer.error:
            raise CheckpointRefusedError(answer.error)
        return RunnableConfig(
            configurable={"thread_id": thread_id, "checkpoint_ns": namespace, "checkpoint_id": checkpoint["id"]}
        )

    async def aput_writes(
        self, config: RunnableConfig, writes: Sequence[tuple[str, Any]], task_id: str, task_path: str = ""
    ) -> None:
        _, namespace = _thread(config)
        pending = []
        for i, (channel, value) in enumerate(writes):
            value_type, data = self.serde.dumps_typed(value)
            pending.append(
                pb.PendingWrite(
                    task_id=task_id,
                    channel=channel,
                    idx=WRITES_IDX_MAP.get(channel, i),
                    value=pb.TypedValue(type=value_type, data=data),
                    task_path=task_path,
                )
            )
        request_id = _request_id()
        put = pb.CheckpointWritesRequest(
            request_id=request_id,
            checkpoint_ns=namespace,
            checkpoint_id=get_checkpoint_id(config) or "",
            writes=pending,
        )
        answer = (await self._channel.request(pb.ConnectRequest(checkpoint_writes=put), request_id)).checkpoint_writes
        if answer.error:
            raise CheckpointRefusedError(answer.error)

    def get_next_version(self, current: str | None, channel: None) -> str:
        """Versions as InMemorySaver writes them: a counter, then a random part so two writers never collide."""
        number = 0 if current is None else int(current) if isinstance(current, int) else int(current.split(".")[0])
        return f"{number + 1:032}.{random.random():016}"  # noqa: S311 - a tiebreak, not a secret


def _thread(config: RunnableConfig) -> tuple[str, str]:
    configurable = config.get("configurable", {})
    return str(configurable["thread_id"]), str(configurable.get("checkpoint_ns", ""))


def _request_id() -> str:
    from rotor_runner.control import request_id

    return request_id()
