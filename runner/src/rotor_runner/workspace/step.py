"""A workspace step's life (#136): the run's branch fetched and checked out at its start, what it wrote committed
and pushed at its end when it writes and succeeded, and nothing kept after it."""

from __future__ import annotations

import asyncio
import contextlib
import json
import time
from collections.abc import AsyncIterator
from dataclasses import dataclass, field
from pathlib import Path
from typing import TYPE_CHECKING, Any

from rotor_runner.config import (
    COMMAND_DEFAULT_TIMEOUT_S,
    GIT_ROOT,
    MAX_WORKSPACE_STEPS,
    SUMMARY_CHARS,
    WORK_TREE,
    WORKSPACE_STEP_DEFAULT_S,
)
from rotor_runner.events import EventSink
from rotor_runner.limits import failed
from rotor_runner.templates import render
from rotor_runner.workspace import git
from rotor_runner.workspace.agent import AgentClient, CommandResult
from rotor_runner.workspace.files import WorkspaceError, WorkTree, remove_tree

if TYPE_CHECKING:
    from dulwich.objects import ObjectID

    from rotor_runner.compile import RunContext

# The loopback address every agent of the sandbox listens on, each at the port the worker gave its image.
AGENT_HOST = "127.0.0.1"


@dataclass(frozen=True)
class WorkspaceConfig:
    """What the worker told the runner of its sandbox: which agent serves which image, the secret they take, and how
    long a step may take."""

    agents: dict[str, str]
    secret: str = field(repr=False)
    step_seconds: float = WORKSPACE_STEP_DEFAULT_S
    tree_root: Path = Path(WORK_TREE)
    git_root: Path = Path(GIT_ROOT)

    @classmethod
    def from_env(cls, told: str, secret: str) -> WorkspaceConfig | None:
        """The sandbox's workspace, or None for a sandbox started without toolchains."""
        if not told or not secret:
            return None
        data = json.loads(told)
        agents = {str(name): f"http://{AGENT_HOST}:{int(port)}" for name, port in dict(data["agents"]).items()}
        return cls(agents, secret, float(data.get("stepSeconds") or WORKSPACE_STEP_DEFAULT_S))


class Workspace:
    """One step's work tree and toolchain, as its agent's tools and its code node's ctx.workspace reach them."""

    def __init__(self, tree: WorkTree, agent: AgentClient, writes: bool, deadline: float, emit: EventSink, node: str):
        self.tree, self.writes = tree, writes
        self._agent, self._deadline, self._emit, self._node = agent, deadline, emit, node

    def list_files(self, path: str = ".") -> list[str]:
        return self.tree.list_files(path)

    def read_file(self, path: str, start_line: int = 1) -> str:
        return self.tree.read_text(path, start_line)

    def search(self, text: str, path: str = ".") -> list[str]:
        return self.tree.search(text, path)

    def write_file(self, path: str, content: str) -> int:
        self._writing()
        return self.tree.write_text(path, content)

    def edit_file(self, path: str, old: str, new: str, replace_all: bool = False) -> int:
        self._writing()
        return self.tree.edit(path, old, new, replace_all)

    def _writing(self) -> None:
        if not self.writes:
            raise WorkspaceError("This step only reads its workspace; give it access: write to change files.")

    async def run_command(self, argv: list[str], cwd: str = ".", timeout_s: float | None = None) -> CommandResult:
        """Runs argv in the toolchain container, in a directory of the work tree, for at most the step's time left."""
        left = self._deadline - time.monotonic()
        if left <= 1:
            raise WorkspaceError("The step has no time left to run a command.")
        timeout = min(float(timeout_s or COMMAND_DEFAULT_TIMEOUT_S), left)
        result = await self._agent.run(argv, str(self.tree.path_of(cwd)), timeout)
        await self._emit(
            "workspace.command",
            self._node,
            {
                "argv": argv,
                "exitCode": result.exit_code,
                "timedOut": result.timed_out,
                "durationMs": result.duration_ms,
                "outputTail": result.output[-SUMMARY_CHARS:],
            },
        )
        return result

    async def call(self, name: str, arguments: dict[str, Any]) -> Any:
        """One call a code node relays, by the name of the agent's tool, answered as JSON."""
        args = dict(arguments)
        if name == "run_command":
            # argv goes on as it came, so a shell line is refused rather than split into letters.
            result = await self.run_command(args.get("argv"), str(args.get("cwd") or "."), args.get("timeout"))  # type: ignore[arg-type]
            return {
                "exitCode": result.exit_code,
                "timedOut": result.timed_out,
                "durationMs": result.duration_ms,
                "output": result.output,
                "truncated": result.truncated,
            }
        calls = {
            "list_files": self.list_files,
            "read_file": self.read_file,
            "search": self.search,
            "write_file": self.write_file,
            "edit_file": self.edit_file,
        }
        if name not in calls:
            raise WorkspaceError(f"A workspace has no call {name}.")
        try:
            return calls[name](**args)  # type: ignore[operator]
        except TypeError as err:
            raise WorkspaceError(f"{name} was called with arguments it does not take: {err}.") from None


class Workspaces:
    """The run's workspace steps in this sandbox: they are counted, and each gets a checkout of its own."""

    def __init__(self, config: WorkspaceConfig | None, run_id: str, emit: EventSink) -> None:
        self.config, self.run_id, self.emit = config, run_id, emit
        self.steps = 0

    @contextlib.asynccontextmanager
    async def step(self, node: dict[str, Any], state: dict[str, Any], ctx: RunContext) -> AsyncIterator[Workspace]:
        """The node's workspace for the length of the step; it pushes only when the step ended well and writes."""
        spec, node_id = node["workspace"], node["id"]
        cfg = self.config
        if cfg is None:
            raise failed("workspace_unavailable", f"{node_id} needs a workspace, and this sandbox has no toolchain")
        self.steps += 1
        if self.steps > MAX_WORKSPACE_STEPS:
            raise failed("workspace_steps", f"the run took more than {MAX_WORKSPACE_STEPS} workspace steps")
        url = cfg.agents.get(spec["image"])
        if url is None:
            raise failed("workspace_unavailable", f"this sandbox has no toolchain {spec['image']} for {node_id}")
        branch = render(spec["branch"], state)
        if not isinstance(branch, str) or not branch:
            raise failed("workspace_branch", f"the branch of {node_id} is not a name: {branch!r}")
        access = await ctx.repository(spec["repository"])
        writes = spec.get("access") == "write"
        if writes and not access.writes:
            raise failed("workspace_binding", f"{node_id} writes, and {spec['repository']} may not push")
        name = f"{self.steps:02d}-{node_id}"
        tree_dir, git_dir = cfg.tree_root / name, cfg.git_root / name
        agent = AgentClient(url, cfg.secret)
        try:
            async with asyncio.timeout(cfg.step_seconds):
                await agent.ready()
                tree_dir.mkdir()
                repo, head = await asyncio.to_thread(git.fetch, access.url, access.headers(), branch, git_dir)
                await asyncio.to_thread(git.checkout, repo, head, tree_dir)
                await self.emit(
                    "workspace.checkout", node_id, {"branch": branch, "commit": head.decode(), "image": spec["image"]}
                )
                tree = WorkTree(tree_dir)
                yield Workspace(tree, agent, writes, time.monotonic() + cfg.step_seconds, self.emit, node_id)
                if writes:
                    await self._push(repo, head, tree, ctx, spec["repository"], branch, node_id)
        except TimeoutError:
            raise failed("workspace_timeout", f"{node_id} took longer than its {cfg.step_seconds:g} seconds") from None
        except WorkspaceError as err:
            raise failed("workspace_error", f"{node_id}: {err}") from err
        finally:
            with contextlib.suppress(WorkspaceError):
                await agent.reset()
            remove_tree(tree_dir)
            remove_tree(git_dir)

    async def _push(
        self, repo: Any, head: ObjectID, tree: WorkTree, ctx: RunContext, binding: str, branch: str, node_id: str
    ) -> None:
        """Commits what changed and pushes it, with the binding's token as it is now, a long step's having expired."""
        message = f"{node_id}: what a step of a Rotor run wrote\n\nRun {self.run_id}, step {node_id}.\n"
        made = await asyncio.to_thread(git.commit, repo, head, tree, message)
        if made is None:
            await self.emit("workspace.unchanged", node_id, {"branch": branch})
            return
        access = await ctx.repository(binding)
        await asyncio.to_thread(git.push, repo, access.url, access.headers(), branch, head, made)
        await self.emit(
            "workspace.pushed", node_id, {"branch": branch, "commit": made.decode(), "parent": head.decode()}
        )
