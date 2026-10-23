"""The run's branch on dulwich, in the runner alone (#136): the token never reaches the toolchain, and the git
directory stays where only the runner sees it, so no hook the repository brings ever runs."""

from __future__ import annotations

import time
from collections.abc import Callable, Iterator
from pathlib import Path
from typing import Any

from dulwich.client import GitClient, HttpGitClient, get_transport_and_path
from dulwich.ignore import IgnoreFilter
from dulwich.index import build_index_from_tree, commit_tree
from dulwich.object_store import iter_tree_contents
from dulwich.objects import Blob, Commit, ObjectID
from dulwich.refs import Ref, check_ref_format
from dulwich.repo import Repo

from rotor_runner.config import COMMIT_AUTHOR, GIT_TIMEOUT_S, MAX_COMMIT_FILE_BYTES, MAX_WRITE_BYTES, OWNED_DIR_MODE
from rotor_runner.workspace.files import WorkspaceError, WorkTree

# The user name git sends with the token; Armature's proxy reads the password alone.
GIT_USER = "rotor"
# git's modes for a file, an executable and a symbolic link, which a commit keeps as a link.
MODE_FILE, MODE_EXECUTABLE, MODE_LINK = 0o100644, 0o100755, 0o120000
_ANY_EXECUTE = 0o111


def branch_ref(branch: str) -> Ref:
    """The ref of a branch, refusing a name git would not take."""
    ref = Ref(b"refs/heads/" + branch.encode())
    if not branch or not check_ref_format(ref):
        raise WorkspaceError(f"{branch!r} is not a branch name git accepts.")
    return ref


def _client(url: str, token: str) -> tuple[GitClient, str]:
    """The git proxy over HTTP, the token as basic auth's password, never in the address; dulwich sends it to that
    origin alone, so a redirect elsewhere never carries it. A path is a repository on disk, for the runner's tests."""
    if url.startswith(("https://", "http://")):
        return HttpGitClient(url, username=GIT_USER, password=token, timeout=GIT_TIMEOUT_S), ""
    return get_transport_and_path(url)


def fetch(url: str, token: str, branch: str, git_dir: Path) -> tuple[Repo, ObjectID]:
    """Fetches only the branch, at depth 1, into a new git directory: one ref is all a step needs, and a whole
    clone of a long history fetches every tag besides."""
    ref = branch_ref(branch)
    git_dir.mkdir(mode=OWNED_DIR_MODE)
    repo = Repo.init_bare(str(git_dir))
    client, path = _client(url, token)

    def wants(refs: dict[Ref, ObjectID], depth: int | None = None) -> list[ObjectID]:
        if ref not in refs:
            raise WorkspaceError(f"The repository has no branch {branch}; make it before a step works on it.")
        return [refs[ref]]

    try:
        result = client.fetch(path, repo, determine_wants=wants, depth=1)  # type: ignore[arg-type]
    except WorkspaceError:
        raise
    except Exception as err:
        raise WorkspaceError(f"The branch {branch} could not be fetched: {err}") from err
    head = result.refs[ref]
    if head is None:
        raise WorkspaceError(f"The repository has no branch {branch}; make it before a step works on it.")
    repo.refs[ref] = head
    repo.refs.set_symbolic_ref(Ref(b"HEAD"), ref)
    return repo, head


def checkout(repo: Repo, commit: ObjectID, work_tree: Path) -> None:
    """Writes the commit's files into the work tree, which holds no .git; the index stays in the git directory."""
    tree = repo[commit].tree  # type: ignore[attr-defined]
    build_index_from_tree(str(work_tree), repo.index_path(), repo.object_store, tree)


class _Ignored:
    """The work tree's .gitignore files, each read only when it is a regular file, so a planted link reads nothing."""

    def __init__(self, tree: WorkTree) -> None:
        self.tree = tree
        self.filters: list[tuple[str, IgnoreFilter]] = []

    def enter(self, directory: str) -> None:
        self.filters = [(d, f) for d, f in self.filters if directory == d or directory.startswith(d + "/") or d == ""]
        try:
            data, _ = self.tree.read_bytes(f"{directory}/.gitignore" if directory else ".gitignore", MAX_WRITE_BYTES)
        except WorkspaceError:
            return
        self.filters.append((directory, IgnoreFilter(data.splitlines(), path=None)))

    def ignored(self, rel: str, is_dir: bool) -> bool:
        for base, f in reversed(self.filters):
            inside = rel[len(base) + 1 :] if base else rel
            verdict = f.is_ignored(inside + "/" if is_dir else inside)
            if verdict is not None:
                return verdict
        return False


def _entries(tree: WorkTree, tracked: set[bytes]) -> Iterator[tuple[bytes, Blob, int]]:
    """What the work tree holds as git would commit it: tracked paths, and the rest unless .gitignore says not."""
    ignore = _Ignored(tree)
    skipped: list[str] = []
    ignore.enter("")
    for rel, entry in tree.walk("."):
        if any(rel.startswith(s + "/") for s in skipped):
            continue
        name = rel.encode()
        is_dir = entry.is_dir(follow_symlinks=False)
        wanted = name in tracked or (is_dir and any(t.startswith(name + b"/") for t in tracked))
        if not wanted and ignore.ignored(rel, is_dir):
            if is_dir:
                skipped.append(rel)
            continue
        if is_dir:
            ignore.enter(rel)
        elif entry.is_symlink():
            yield name, Blob.from_string(tree.read_link(rel)), MODE_LINK
        elif entry.is_file(follow_symlinks=False):
            data, more = tree.read_bytes(rel, MAX_COMMIT_FILE_BYTES)
            if more:
                raise WorkspaceError(
                    f"{rel} is larger than git should carry through a step; leave it out with .gitignore."
                )
            mode = entry.stat(follow_symlinks=False).st_mode
            yield name, Blob.from_string(data), MODE_EXECUTABLE if mode & _ANY_EXECUTE else MODE_FILE


def commit(repo: Repo, parent: ObjectID, tree: WorkTree, message: str) -> ObjectID | None:
    """Commits what the work tree holds on top of parent, a link as a link; None when nothing changed."""
    parent_tree = repo[parent].tree  # type: ignore[attr-defined]
    tracked = {e.path for e in iter_tree_contents(repo.object_store, parent_tree) if e.path is not None}
    blobs: list[tuple[bytes, Any, int]] = []
    for path, blob, mode in _entries(tree, tracked):
        repo.object_store.add_object(blob)
        blobs.append((path, blob.id, mode))
    new_tree = commit_tree(repo.object_store, blobs)
    if new_tree == parent_tree:
        return None
    c = Commit()
    c.tree, c.parents, c.message = new_tree, [parent], message.encode()
    c.author = c.committer = COMMIT_AUTHOR
    c.author_time = c.commit_time = int(time.time())
    c.author_timezone = c.commit_timezone = 0
    repo.object_store.add_object(c)
    return c.id


def push(repo: Repo, url: str, token: str, branch: str, parent: ObjectID, new: ObjectID) -> None:
    """Pushes new onto parent; a branch that moved on meanwhile is never overwritten, and a refusal is said as the
    host said it."""
    ref = branch_ref(branch)
    client, path = _client(url, token)

    def update(refs: dict[Ref, ObjectID]) -> dict[Ref, ObjectID]:
        if refs.get(ref) != parent:
            raise WorkspaceError(f"The branch {branch} moved on while the step worked, so its work was not pushed.")
        return {**refs, ref: new}

    generate: Callable[..., Any] = lambda have, want, **kw: repo.generate_pack_data(  # noqa: E731
        have, want, shallow=repo.get_shallow(), **kw
    )
    try:
        result = client.send_pack(path.encode(), update, generate)
    except WorkspaceError:
        raise
    except Exception as err:
        raise WorkspaceError(f"The branch {branch} could not be pushed: {err}") from err
    refused = (result.ref_status or {}).get(ref)
    if refused:
        raise WorkspaceError(f"The host refused the push of {branch}: {refused}")
    repo.refs[ref] = new
