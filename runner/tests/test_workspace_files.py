"""A step's work tree (#136): paths stay inside it, no call follows a symbolic link the toolchain planted, and what is
read, listed or searched is bounded."""

from __future__ import annotations

import os
from pathlib import Path

import pytest

from rotor_runner.config import MAX_READ_BYTES
from rotor_runner.workspace.files import WorkspaceError, WorkTree, remove_tree

SECRET_TEXT = "ROTOR_RUNNER_BOOTSTRAP=what-a-planted-link-would-leak"


@pytest.fixture
def tree(tmp_path: Path) -> WorkTree:
    root = tmp_path / "tree"
    (root / "src").mkdir(parents=True)
    (root / "src" / "main.go").write_text("package main\n\nfunc main() {}\n")
    (root / "README.md").write_text("# portal\n")
    outside = tmp_path / "runner-environment"
    outside.write_text(SECRET_TEXT)
    (root / "leak").symlink_to(outside)
    (root / "away").symlink_to(tmp_path)
    return WorkTree(root)


def test_a_planted_link_is_never_followed(tree: WorkTree) -> None:
    for read in (lambda: tree.read_text("leak"), lambda: tree.read_bytes("away/runner-environment")):
        with pytest.raises(WorkspaceError, match="symbolic link"):
            read()
    with pytest.raises(WorkspaceError, match="symbolic link"):
        tree.write_text("leak", "overwritten")
    with pytest.raises(WorkspaceError, match="symbolic link"):
        tree.write_text("away/planted.txt", "outside the tree")
    with pytest.raises(WorkspaceError, match="symbolic link"):
        tree.edit("leak", "what", "who")
    with pytest.raises(WorkspaceError, match="symbolic link"):
        tree.path_of("away")
    assert (tree.root.parent / "runner-environment").read_text() == SECRET_TEXT
    assert not (tree.root.parent / "planted.txt").exists()
    listed = tree.list_files()
    assert "leak@" in listed and "away@" in listed and "src/" in listed and "src/main.go" in listed
    assert not any("runner-environment" in entry for entry in listed)
    assert all(SECRET_TEXT not in line for line in tree.search("ROTOR_RUNNER"))


def test_a_path_stays_inside_the_work_tree(tree: WorkTree) -> None:
    for path in ("/etc/passwd", "../runner-environment", "src/../../runner-environment", "a\x00b"):
        with pytest.raises(WorkspaceError):
            tree.read_text(path)
    with pytest.raises(WorkspaceError, match=r"\.git"):
        tree.write_text(".git/hooks/post-commit", "#!/bin/sh\n")


def test_a_fifo_is_refused_without_waiting_for_a_writer(tree: WorkTree) -> None:
    os.mkfifo(tree.root / "pipe")
    with pytest.raises(WorkspaceError, match="not a regular file"):
        tree.read_text("pipe")


def test_files_are_written_with_their_directories_and_edited_exactly_once(tree: WorkTree) -> None:
    assert tree.write_text("pkg/passkey/passkey.go", "package passkey\n\nconst On = false\n") > 0
    assert tree.edit("pkg/passkey/passkey.go", "On = false", "On = true") == 1
    assert "On = true" in tree.read_text("pkg/passkey/passkey.go")
    tree.write_text("twice.txt", "a a")
    with pytest.raises(WorkspaceError, match="2 times"):
        tree.edit("twice.txt", "a", "b")
    assert tree.edit("twice.txt", "a", "b", replace_all=True) == 2
    with pytest.raises(WorkspaceError, match="does not contain"):
        tree.edit("twice.txt", "missing", "x")


def test_reading_and_searching_are_bounded_and_skip_binaries(tree: WorkTree) -> None:
    line = "x" * 99 + "\n"
    tree.write_text("big.txt", line * (2 * MAX_READ_BYTES // len(line)))
    first = tree.read_text("big.txt")
    assert len(first.encode()) < MAX_READ_BYTES + len(line) * 2 and "read it again from line" in first
    later = int(first.rsplit("from line ", 1)[1].split()[0])
    assert tree.read_text("big.txt", later).startswith("x")
    (tree.root / "blob.bin").write_bytes(b"\x00\x01main")
    with pytest.raises(WorkspaceError, match="binary"):
        tree.read_text("blob.bin")
    assert tree.search("func main") == ["src/main.go:3: func main() {}"]


def test_removing_a_tree_takes_read_only_directories_and_never_follows_a_link(tmp_path: Path) -> None:
    keep = tmp_path / "keep"
    keep.mkdir()
    (keep / "precious.txt").write_text("stays")
    root = tmp_path / "step"
    locked = root / "cache" / "mod" / "example.com@v1"
    locked.mkdir(parents=True)
    (locked / "go.mod").write_text("module m\n")
    locked.chmod(0o555)
    (root / "out").symlink_to(keep)
    remove_tree(root)
    assert not root.exists()
    assert (keep / "precious.txt").read_text() == "stays"
