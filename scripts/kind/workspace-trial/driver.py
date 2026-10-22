"""The trial of #132, reverted once docs/decisions.md records it: the runner's side of a workspace step, on dulwich,
with the git directory where only the runner sees it and the work tree on the volume it shares with the toolchain."""

from __future__ import annotations

import http.client
import json
import os
import resource
import shutil
import socket
import sys
import time

from dulwich import porcelain
from dulwich.client import get_transport_and_path
from dulwich.index import build_index_from_tree
from dulwich.repo import Repo

GIT_DIR = "/private/repo"
WORKSPACE = "/workspace"
SOCKET = "/control/agent.sock"
AUTHOR = b"Rotor trial <trial@rotor.invalid>"
HTTP_SLACK_SECONDS = 30
MAX_OUTPUT = 64 << 10
PRINT_TAIL = 1500
HEAD_CHARS = 120
MB = 1e6
MODES = {
    "full": {},
    "shallow": {"depth": 1},
    "blobless": {"filter_spec": "blob:none"},
    "shallow-blobless": {"depth": 1, "filter_spec": "blob:none"},
}


def size_of(root: str) -> tuple[int, int]:
    total, files = 0, 0
    for dirpath, _, names in os.walk(root):
        for name in names:
            st = os.lstat(os.path.join(dirpath, name))
            total, files = total + st.st_size, files + 1
    return total, files


def empty(root: str) -> None:
    for name in os.listdir(root):
        path = os.path.join(root, name)
        if os.path.isdir(path) and not os.path.islink(path):
            shutil.rmtree(path)
        else:
            os.unlink(path)


def report(**values: object) -> None:
    values["runnerMaxRssKB"] = resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
    print(json.dumps(values))


def fetch_branch(url: str, branch: str, depth: int | None) -> Repo:
    """Fetches the one branch, where porcelain's clone fetches every ref and tag, each to the depth asked."""
    ref = b"refs/heads/" + branch.encode()
    client, path = get_transport_and_path(url)
    repo = Repo.init_bare(GIT_DIR, mkdir=True)
    result = client.fetch(path, repo, determine_wants=lambda refs, *_, **__: [refs[ref]], depth=depth)
    repo.refs[ref] = result.refs[ref]
    repo.refs.set_symbolic_ref(b"HEAD", ref)
    return repo


def clone(name: str, branch: str, mode: str) -> None:
    url = os.environ["FORGE_URL"] + "/" + name
    shutil.rmtree(GIT_DIR, ignore_errors=True)
    empty(WORKSPACE)
    start = time.monotonic()
    if mode.startswith("branch"):
        repo = fetch_branch(url, branch, None if mode == "branch-full" else 1)
    else:
        with open(os.devnull, "wb") as quiet:
            repo = porcelain.clone(url, GIT_DIR, bare=True, branch=branch, errstream=quiet, **MODES[mode])
    cloned = time.monotonic()
    tree = repo[repo.head()].tree
    problem = ""
    try:
        build_index_from_tree(WORKSPACE, repo.index_path(), repo.object_store, tree)
    except KeyError as err:
        problem = f"checkout misses object {err}"
    checked = time.monotonic()
    repo.close()
    git_bytes, _ = size_of(GIT_DIR)
    ws_bytes, ws_files = size_of(WORKSPACE)
    report(
        step="clone",
        mode=mode,
        cloneSeconds=round(cloned - start, 2),
        checkoutSeconds=round(checked - cloned, 2),
        gitDirMB=round(git_bytes / MB, 1),
        workspaceMB=round(ws_bytes / MB, 1),
        workspaceFiles=ws_files,
        problem=problem,
    )


class UnixConnection(http.client.HTTPConnection):
    def __init__(self, path: str, timeout: float) -> None:
        super().__init__("agent", timeout=timeout)
        self._path = path

    def connect(self) -> None:
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self._path)


def run(directory: str, timeout: int, argv: list[str], env: list[str]) -> dict[str, object]:
    conn = UnixConnection(SOCKET, timeout + HTTP_SLACK_SECONDS)
    body = {"argv": argv, "dir": directory, "env": env, "timeoutSeconds": timeout, "maxOutputBytes": MAX_OUTPUT}
    start = time.monotonic()
    conn.request("POST", "/exec", json.dumps(body), {"Content-Type": "application/json"})
    answer: dict[str, object] = json.loads(conn.getresponse().read())
    answer["roundTripMs"] = round((time.monotonic() - start) * 1000)
    return answer


def execute(directory: str, timeout: str, *rest: str) -> None:
    split = rest.index("--")
    env, argv = list(rest[:split]), list(rest[split + 1 :])
    answer = run(directory, int(timeout), argv, env)
    output = str(answer.pop("output"))
    report(step="exec", argv=argv, **answer)
    print(output[-PRINT_TAIL:])


def push(name: str, branch: str, message: str) -> None:
    url = os.environ["FORGE_URL"] + "/" + name
    start = time.monotonic()
    repo = Repo(GIT_DIR)
    # The work tree is the shared volume, the git directory stays where only the runner sees it.
    repo.path, repo.bare = WORKSPACE, False
    status = porcelain.status(repo, untracked_files="all")
    changed = [os.path.join(WORKSPACE, os.fsdecode(p)) for p in list(status.unstaged) + list(status.untracked)]
    porcelain.add(repo, changed)
    commit = porcelain.commit(repo, message=message.encode(), author=AUTHOR, committer=AUTHOR)
    committed = time.monotonic()
    with open(os.devnull, "wb") as quiet:
        porcelain.push(repo, url, [b"HEAD:refs/heads/" + branch.encode()], outstream=quiet, errstream=quiet)
    pushed = time.monotonic()
    repo.close()
    report(
        step="push",
        branch=branch,
        commit=commit.decode(),
        changed=len(changed),
        commitSeconds=round(committed - start, 2),
        pushSeconds=round(pushed - committed, 2),
    )


def edit(path: str, text: str) -> None:
    with open(os.path.join(WORKSPACE, path), "a", encoding="utf-8") as f:
        f.write(text + "\n")
    report(step="edit", path=path)


def read(path: str, safe: str) -> None:
    """Reads a file the toolchain may have replaced by a symlink; safe refuses to follow one."""
    flags = os.O_RDONLY | (os.O_NOFOLLOW if safe == "safe" else 0)
    try:
        fd = os.open(os.path.join(WORKSPACE, path), flags)
        with os.fdopen(fd, "rb") as f:
            data = f.read(PRINT_TAIL)
        report(step="read", path=path, mode=safe, bytes=len(data), head=data[:HEAD_CHARS].decode(errors="replace"))
    except OSError as err:
        report(step="read", path=path, mode=safe, refused=str(err))


def reset() -> None:
    conn = UnixConnection(SOCKET, HTTP_SLACK_SECONDS)
    conn.request("POST", "/reset")
    report(step="reset", **json.loads(conn.getresponse().read()))


if __name__ == "__main__":
    commands = {"clone": clone, "exec": execute, "push": push, "edit": edit, "read": read, "reset": reset}
    commands[sys.argv[1]](*sys.argv[2:])
