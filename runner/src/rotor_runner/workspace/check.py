"""The workspace check (#136): what a sandbox with toolchains must hold, proven inside one by its runner, with a
repository of its own in the git directory, so the Kubernetes and Docker tests need no git host."""

from __future__ import annotations

import time
from pathlib import Path
from typing import Any

from dulwich.index import commit_tree
from dulwich.object_store import iter_tree_contents
from dulwich.objects import Blob, Commit
from dulwich.refs import Ref
from dulwich.repo import Repo

from rotor_runner.events import discard
from rotor_runner.limits import RunEnded
from rotor_runner.repository import Repository as Bound
from rotor_runner.repository import RepositoryAccess
from rotor_runner.workspace.agent import AgentClient
from rotor_runner.workspace.files import WorkspaceError
from rotor_runner.workspace.git import MODE_FILE, MODE_LINK
from rotor_runner.workspace.step import WorkspaceConfig, Workspaces

BRANCH = "rotor/workspace-check"
ANSWER = "workspace-check: ok"
# A process a step leaves behind, out of its process group and its output, which the agent's reset must end; how
# long a probe may run.
STRAY = "sleep 3600"
LEAVE_BEHIND = ["sh", "-c", f"setsid {STRAY} </dev/null >/dev/null 2>&1 &"]
PROBE_S = 10
# The check's origin is a directory of the sandbox, which takes no token.
NO_TOKEN = ""


class CheckFailedError(AssertionError):
    """What the sandbox lacks, as a sentence."""


def _origin(at: Path) -> Path:
    repo = Repo.init_bare(str(at), mkdir=True)
    blob = Blob.from_string(b"# check\n")
    repo.object_store.add_object(blob)
    c = Commit()
    c.tree, c.parents, c.message = commit_tree(repo.object_store, [(b"README.md", blob.id, MODE_FILE)]), [], b"Check\n"
    c.author = c.committer = b"Rotor <rotor@rotor.invalid>"
    c.author_time = c.commit_time = int(time.time())
    c.author_timezone = c.commit_timezone = 0
    repo.object_store.add_object(c)
    repo.refs[Ref(b"refs/heads/" + BRANCH.encode())] = c.id
    repo.close()
    return at


class _Context:
    """What a step asks of its run: the repository binding, here the origin in the git directory."""

    def __init__(self, url: str) -> None:
        self.url = url

    async def repository(self, binding: str) -> RepositoryAccess:
        return RepositoryAccess(binding, Bound("CHECK", "rotor/check", self.url), token=NO_TOKEN, writes=True)


def _expect(ok: bool, what: str) -> None:
    if not ok:
        raise CheckFailedError(what)


async def workspace_check(config: WorkspaceConfig) -> str:
    """Runs two steps in each toolchain: one writes through the tools and the toolchain and pushes, the next sees
    the push and nothing else the first left."""
    origin = _origin(config.git_root / "origin.git")
    ctx: Any = _Context(str(origin))
    steps = Workspaces(config, "check", discard)
    for image, url in sorted(config.agents.items()):
        with_secret = AgentClient(url, "not-the-secret")
        await AgentClient(url, config.secret).ready()
        try:
            await with_secret.run(["true"], str(config.tree_root), PROBE_S)
            raise CheckFailedError(f"the agent of {image} ran a command for a caller without the sandbox's secret")
        except WorkspaceError:
            pass
        node = {
            "id": f"write-{image}",
            "workspace": {"repository": "repo", "branch": BRANCH, "image": image, "access": "write"},
        }
        async with steps.step(node, {}, ctx) as ws:
            ws.write_file("from-runner.txt", "written by the runner\n")
            made = await ws.run_command(
                ["sh", "-c", "cat from-runner.txt > from-toolchain.txt; ln -s /proc/self/environ leak"]
            )
            _expect(made.exit_code == 0, f"the toolchain {image} could not write the work tree: {made.output}")
            _expect(
                ws.read_file("from-toolchain.txt") == "written by the runner\n",
                "the runner does not see what the toolchain wrote",
            )
            try:
                ws.read_file("leak")
                raise CheckFailedError("a link the toolchain planted was followed")
            except WorkspaceError:
                pass
            await ws.run_command(LEAVE_BEHIND)
        node = {
            "id": f"read-{image}",
            "workspace": {"repository": "repo", "branch": BRANCH, "image": image, "access": "read"},
        }
        async with steps.step(node, {}, ctx) as ws:
            _expect("from-toolchain.txt" in ws.list_files("."), "the next step does not see what the last one pushed")
            left = await ws.run_command(["ps"])
            _expect(STRAY not in left.output, f"a process of the last step outlived it: {left.output}")
    repo = Repo(str(origin))
    tip = repo.refs[Ref(b"refs/heads/" + BRANCH.encode())]
    modes = {e.path: e.mode for e in iter_tree_contents(repo.object_store, repo[tip].tree)}  # type: ignore[attr-defined]
    _expect(modes.get(b"leak") == MODE_LINK, "the push did not keep the planted link as a link")
    return ANSWER


def run_check(config: WorkspaceConfig | None) -> int:
    """The check's exit code, saying what failed."""
    import asyncio

    if config is None:
        print("workspace-check: the sandbox has no toolchains; set ROTOR_WORKSPACE and ROTOR_WORKSPACE_SECRET.")
        return 1
    try:
        print(asyncio.run(workspace_check(config)))
    except (CheckFailedError, RunEnded) as err:
        print(f"workspace-check: {err}")
        return 1
    return 0
