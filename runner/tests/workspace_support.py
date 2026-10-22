"""What the workspace tests share: an origin repository on disk, and a stand-in for the exec agent that speaks its
protocol over loopback, so the runner's side is tested whole; backend/cmd/workspace-agent tests the agent itself."""

from __future__ import annotations

import json
import os
import subprocess
import threading
import time
from collections.abc import Iterator
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

from dulwich.index import commit_tree
from dulwich.objects import Blob, Commit
from dulwich.refs import Ref
from dulwich.repo import Repo

BRANCH = "rotor/cp-12"
SECRET = "agent-secret-for-the-tests"
TOKEN = "armature-token-for-the-tests"
AUTHOR = b"Fixture <fixture@rotor.invalid>"
FIXTURE_TIME = 1767225600


def origin(at: Path, files: dict[str, bytes] | None = None) -> Path:
    """A bare repository with one commit on BRANCH and on main: a README, a source file and a .gitignore."""
    repo = Repo.init_bare(str(at), mkdir=True)
    content = files or {
        "README.md": b"# portal\n\nSign in with a passkey.\n",
        "src/app.txt": b"hello\n",
        ".gitignore": b"build/\n*.log\n",
    }
    blobs = []
    for path, data in content.items():
        blob = Blob.from_string(data)
        repo.object_store.add_object(blob)
        blobs.append((path.encode(), blob.id, 0o100644))
    c = Commit()
    c.tree, c.parents, c.message = commit_tree(repo.object_store, blobs), [], b"Start the portal\n"
    c.author = c.committer = AUTHOR
    c.author_time = c.commit_time = FIXTURE_TIME
    c.author_timezone = c.commit_timezone = 0
    repo.object_store.add_object(c)
    repo.refs[Ref(b"refs/heads/main")] = c.id
    repo.refs[Ref(b"refs/heads/" + BRANCH.encode())] = c.id
    repo.close()
    return at


def files_at(at: Path, branch: str = BRANCH) -> dict[str, tuple[int, bytes]]:
    """The files of the branch's tip in a bare repository, with their modes."""
    from dulwich.object_store import iter_tree_contents

    repo = Repo(str(at))
    try:
        tip = repo.refs[Ref(b"refs/heads/" + branch.encode())]
        tree = repo[tip].tree  # type: ignore[attr-defined]
        return {
            e.path.decode(): (e.mode, repo[e.sha].data)  # type: ignore[attr-defined]
            for e in iter_tree_contents(repo.object_store, tree)
            if e.path is not None and e.sha is not None and e.mode is not None
        }
    finally:
        repo.close()


def head_of(at: Path, branch: str = BRANCH) -> bytes:
    repo = Repo(str(at))
    try:
        return repo.refs[Ref(b"refs/heads/" + branch.encode())]
    finally:
        repo.close()


class AgentStandIn:
    """Speaks the exec agent's protocol: argv run without a shell, its output cut to the cap the request names."""

    def __init__(self, root: Path) -> None:
        self.root = root
        self.commands: list[list[str]] = []
        self.resets = 0
        self.lie_about_output = False
        stand_in = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args: Any) -> None:
                pass

            def do_GET(self) -> None:
                self.send_response(204)
                self.end_headers()

            def do_POST(self) -> None:
                if self.headers.get("Authorization") != f"Bearer {SECRET}":
                    self.send_response(401)
                    self.end_headers()
                    return
                body = json.loads(self.rfile.read(int(self.headers.get("Content-Length", "0"))) or b"{}")
                answer = stand_in.execute(body) if self.path == "/exec" else stand_in.reset()
                encoded = json.dumps(answer).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(encoded)))
                self.end_headers()
                self.wfile.write(encoded)

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.url = f"http://127.0.0.1:{self.server.server_address[1]}"
        threading.Thread(target=self.server.serve_forever, daemon=True).start()

    def execute(self, body: dict[str, Any]) -> dict[str, Any]:
        argv, cap = list(body["argv"]), int(body["maxOutputBytes"])
        assert Path(body["dir"]).resolve().is_relative_to(self.root.resolve())
        self.commands.append(argv)
        start = time.monotonic()
        try:
            done = subprocess.run(  # noqa: S603 - argv, never a shell, is what the agent runs
                argv, cwd=body["dir"], capture_output=True, timeout=body["timeoutSeconds"], check=False
            )
            output, code, timed_out = (done.stdout + done.stderr).decode(errors="replace"), done.returncode, False
        except subprocess.TimeoutExpired:
            output, code, timed_out = "", -1, True
        except OSError as err:
            return {"exitCode": -1, "error": f"{argv[0]} could not be started: {err}."}
        truncated = len(output) > cap
        if truncated and not self.lie_about_output:
            output = output[: cap // 2] + f"\n[... {len(output) - cap} bytes left out ...]\n" + output[-cap // 2 :]
        duration = int((time.monotonic() - start) * 1000)
        return {
            "exitCode": code,
            "timedOut": timed_out,
            "durationMs": duration,
            "output": output,
            "truncated": truncated,
        }

    def reset(self) -> dict[str, Any]:
        self.resets += 1
        return {"killed": 0, "reaped": 0, "cleared": 0}

    def close(self) -> None:
        self.server.shutdown()


def walk(root: Path) -> Iterator[str]:
    for dirpath, _dirs, names in os.walk(root):
        for name in names:
            yield os.path.relpath(os.path.join(dirpath, name), root)
