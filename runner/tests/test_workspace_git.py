"""git in the runner (#136): one branch at depth 1, a commit only of what changed, and a push that never overwrites a
branch that moved on."""

from __future__ import annotations

from pathlib import Path

import pytest
from dulwich.objects import Blob, Commit
from dulwich.refs import Ref
from dulwich.repo import Repo
from workspace_support import BRANCH, head_of, origin

from rotor_runner.workspace import git
from rotor_runner.workspace.files import WorkspaceError, WorkTree


def advance(at: Path, name: bytes) -> bytes:
    """Puts a commit on BRANCH in the origin, as another step or a person would."""
    repo = Repo(str(at))
    ref = Ref(b"refs/heads/" + BRANCH.encode())
    parent = repo.refs[ref]
    blob = Blob.from_string(b"elsewhere\n")
    repo.object_store.add_object(blob)
    tree = repo[repo[parent].tree]  # type: ignore[attr-defined]
    tree.add(name, 0o100644, blob.id)  # type: ignore[attr-defined]
    repo.object_store.add_object(tree)
    c = Commit()
    c.tree, c.parents, c.message = tree.id, [parent], b"Elsewhere\n"
    c.author = c.committer = b"Someone <someone@rotor.invalid>"
    c.author_time = c.commit_time = 1767225700
    c.author_timezone = c.commit_timezone = 0
    repo.object_store.add_object(c)
    repo.refs[ref] = c.id
    repo.close()
    return c.id


def test_only_the_branch_is_fetched_at_depth_one_and_the_work_tree_holds_no_git(tmp_path: Path) -> None:
    at = origin(tmp_path / "origin.git")
    first = head_of(at)
    advance(at, b"second.txt")
    repo, head = git.fetch(str(at), "", BRANCH, tmp_path / "git")
    assert head == head_of(at)
    assert first not in set(repo.object_store), "the history behind the tip stays on the host"
    assert set(repo.refs.keys()) == {Ref(b"HEAD"), Ref(b"refs/heads/" + BRANCH.encode())}
    tree = tmp_path / "tree"
    tree.mkdir()
    git.checkout(repo, head, tree)
    assert sorted(p.name for p in tree.iterdir()) == [".gitignore", "README.md", "second.txt", "src"]


def test_nothing_changed_commits_nothing(tmp_path: Path) -> None:
    at = origin(tmp_path / "origin.git")
    repo, head = git.fetch(str(at), "", BRANCH, tmp_path / "git")
    (tmp_path / "tree").mkdir()
    git.checkout(repo, head, tmp_path / "tree")
    assert git.commit(repo, head, WorkTree(tmp_path / "tree"), "nothing") is None


def test_a_push_never_overwrites_a_branch_that_moved_on(tmp_path: Path) -> None:
    at = origin(tmp_path / "origin.git")
    repo, head = git.fetch(str(at), "", BRANCH, tmp_path / "git")
    (tmp_path / "tree").mkdir()
    git.checkout(repo, head, tmp_path / "tree")
    WorkTree(tmp_path / "tree").write_text("mine.txt", "mine\n")
    made = git.commit(repo, head, WorkTree(tmp_path / "tree"), "mine")
    assert made is not None
    theirs = advance(at, b"theirs.txt")
    with pytest.raises(WorkspaceError, match="moved on"):
        git.push(repo, str(at), "", BRANCH, head, made)
    assert head_of(at) == theirs


def test_a_branch_name_git_refuses_is_refused(tmp_path: Path) -> None:
    for name in ("", "a..b", "with space", "end."):
        with pytest.raises(WorkspaceError):
            git.branch_ref(name)
