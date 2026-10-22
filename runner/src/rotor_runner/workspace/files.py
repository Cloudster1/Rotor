"""A step's work tree as the runner reads and writes it (#136): every path stays inside it and no call follows a
symbolic link, since the toolchain shares the volume and can plant one pointing at the runner's own files."""

from __future__ import annotations

import contextlib
import errno
import os
import shutil
import stat
from collections.abc import Iterator
from pathlib import Path, PurePosixPath

from rotor_runner.config import (
    BINARY_SNIFF_BYTES,
    DIR_MODE,
    FILE_MODE,
    MAX_LISTED_FILES,
    MAX_MATCH_CHARS,
    MAX_READ_BYTES,
    MAX_SEARCH_FILE_BYTES,
    MAX_SEARCH_MATCHES,
    MAX_WRITE_BYTES,
    OWNED_DIR_MODE,
)

_DIR_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
# A FIFO planted by the toolchain must not hang a read, so files open without blocking and are checked regular.
_READ_FLAGS = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC
_WRITE_FLAGS = os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW | os.O_CLOEXEC
_LINK_ERRORS = (errno.ELOOP, errno.ENOTDIR, errno.EMLINK)


class WorkspaceError(Exception):
    """A workspace call refused or failed; the message is a sentence an agent or a person can act on."""


def _parts(path: str) -> list[str]:
    """A path's parts inside the work tree, refusing one that is absolute or climbs out."""
    if "\x00" in path:
        raise WorkspaceError("A path may not hold a NUL byte.")
    pure = PurePosixPath(path or ".")
    if pure.is_absolute():
        raise WorkspaceError(f"{path} is absolute; give a path inside the work tree, such as src/main.go.")
    parts = [p for p in pure.parts if p not in ("", ".")]
    if ".." in parts:
        raise WorkspaceError(f"{path} climbs out with ..; give a path inside the work tree.")
    return parts


def _refused(path: str, err: OSError) -> WorkspaceError:
    if err.errno in _LINK_ERRORS:
        return WorkspaceError(f"{path} is or passes through a symbolic link, which the workspace tools never follow.")
    if err.errno == errno.ENOENT:
        return WorkspaceError(f"There is no {path} in the work tree.")
    if err.errno == errno.EISDIR:
        return WorkspaceError(f"{path} is a directory.")
    return WorkspaceError(f"{path} could not be used: {err.strerror}.")


class WorkTree:
    """One step's checkout, reached through directory descriptors opened without following links."""

    def __init__(self, root: Path) -> None:
        self.root = root

    def path_of(self, path: str) -> Path:
        """Where a directory inside the tree is, checked to be one reached without links, for a command's cwd."""
        parts = _parts(path)
        with self._dir(parts, path):
            return self.root.joinpath(*parts)

    @contextlib.contextmanager
    def _dir(self, parts: list[str], shown: str, create: bool = False) -> Iterator[int]:
        fd = os.open(self.root, _DIR_FLAGS)
        try:
            for name in parts:
                try:
                    if create:
                        with contextlib.suppress(FileExistsError):
                            os.mkdir(name, DIR_MODE, dir_fd=fd)
                    following = os.open(name, _DIR_FLAGS, dir_fd=fd)
                except OSError as err:
                    raise _refused(shown, err) from None
                os.close(fd)
                fd = following
            yield fd
        finally:
            os.close(fd)

    def read_bytes(self, path: str, limit: int = MAX_READ_BYTES) -> tuple[bytes, bool]:
        """Up to limit bytes of a regular file, and whether there is more."""
        parts = _parts(path)
        if not parts:
            raise WorkspaceError("Name a file to read.")
        with self._dir(parts[:-1], path) as parent:
            try:
                fd = os.open(parts[-1], _READ_FLAGS, dir_fd=parent)
            except OSError as err:
                raise _refused(path, err) from None
            with os.fdopen(fd, "rb") as f:
                if not stat.S_ISREG(os.fstat(fd).st_mode):
                    raise WorkspaceError(f"{path} is not a regular file.")
                data = f.read(limit + 1)
        return data[:limit], len(data) > limit

    def read_link(self, path: str) -> bytes:
        """Where a symbolic link points, read without following it, as a commit keeps it."""
        parts = _parts(path)
        with self._dir(parts[:-1], path) as parent:
            try:
                return os.fsencode(os.readlink(parts[-1], dir_fd=parent))
            except OSError as err:
                raise _refused(path, err) from None

    def read_text(self, path: str, start_line: int = 1) -> str:
        """A text file from start_line on, at most MAX_READ_BYTES of it, saying where to read on when it is cut."""
        data, more = self.read_bytes(path, MAX_WRITE_BYTES)
        if b"\x00" in data[:BINARY_SNIFF_BYTES]:
            raise WorkspaceError(f"{path} is a binary file; read text files only.")
        lines = data.decode("utf-8", errors="replace").splitlines(keepends=True)
        out: list[str] = []
        size, line = 0, max(start_line, 1)
        for text in lines[line - 1 :]:
            size += len(text.encode())
            if out and size > MAX_READ_BYTES:
                return "".join(out) + f"\n[... the file goes on; read it again from line {line} ...]\n"
            out.append(text)
            line += 1
        if more:
            out.append(f"\n[... the file is larger than {MAX_WRITE_BYTES} bytes; the rest is not read ...]\n")
        return "".join(out)

    def write_text(self, path: str, content: str) -> int:
        """Writes a file, making its directories, never through a link and never into a .git directory."""
        parts = _parts(path)
        data = content.encode()
        if not parts:
            raise WorkspaceError("Name a file to write.")
        if ".git" in parts:
            raise WorkspaceError("The work tree holds no .git; the runner commits what changed itself.")
        if len(data) > MAX_WRITE_BYTES:
            raise WorkspaceError(f"{path} would be {len(data)} bytes; write at most {MAX_WRITE_BYTES} at once.")
        with self._dir(parts[:-1], path, create=True) as parent:
            try:
                fd = os.open(parts[-1], _WRITE_FLAGS, FILE_MODE, dir_fd=parent)
            except OSError as err:
                raise _refused(path, err) from None
            with os.fdopen(fd, "wb") as f:
                f.write(data)
        return len(data)

    def edit(self, path: str, old: str, new: str, replace_all: bool = False) -> int:
        """Replaces old by new in a text file: exactly once unless replace_all, so an edit lands where it was meant."""
        data, more = self.read_bytes(path, MAX_WRITE_BYTES)
        if more:
            raise WorkspaceError(f"{path} is larger than {MAX_WRITE_BYTES} bytes; edit smaller files.")
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError:
            raise WorkspaceError(f"{path} is not UTF-8 text; edit text files only.") from None
        found = text.count(old) if old else 0
        if found == 0:
            raise WorkspaceError(f"{path} does not contain the text to replace; read it and give the text exactly.")
        if found > 1 and not replace_all:
            raise WorkspaceError(f"{path} contains the text {found} times; give more of it, or replace all.")
        self.write_text(path, text.replace(old, new) if replace_all else text.replace(old, new, 1))
        return found if replace_all else 1

    def walk(self, path: str = ".") -> Iterator[tuple[str, os.DirEntry[str]]]:
        """Every entry under path, depth first in name order, with its path; links are named, never entered."""
        parts = _parts(path)
        base = "/".join(parts)
        with self._dir(parts, path) as fd:
            yield from self._walk(fd, base)

    def _walk(self, fd: int, base: str) -> Iterator[tuple[str, os.DirEntry[str]]]:
        with os.scandir(fd) as it:
            entries = sorted(it, key=lambda e: e.name)
        for entry in entries:
            rel = f"{base}/{entry.name}" if base else entry.name
            yield rel, entry
            if entry.is_dir(follow_symlinks=False):
                try:
                    child = os.open(entry.name, _DIR_FLAGS, dir_fd=fd)
                except OSError:
                    continue
                try:
                    yield from self._walk(child, rel)
                finally:
                    os.close(child)

    def list_files(self, path: str = ".") -> list[str]:
        """The entries under path: a directory ends in /, a symbolic link in @, as ls -F shows them."""
        out: list[str] = []
        for rel, entry in self.walk(path):
            if len(out) >= MAX_LISTED_FILES:
                out.append(f"[... more than {MAX_LISTED_FILES} entries; list a directory inside ...]")
                break
            suffix = "/" if entry.is_dir(follow_symlinks=False) else "@" if entry.is_symlink() else ""
            out.append(rel + suffix)
        return out

    def search(self, text: str, path: str = ".") -> list[str]:
        """Lines holding text, as path:line: text, in the regular text files under path."""
        if not text:
            raise WorkspaceError("Give the text to search for.")
        out: list[str] = []
        for rel, entry in self.walk(path):
            if (
                not entry.is_file(follow_symlinks=False)
                or entry.stat(follow_symlinks=False).st_size > MAX_SEARCH_FILE_BYTES
            ):
                continue
            try:
                data, _ = self.read_bytes(rel, MAX_SEARCH_FILE_BYTES)
            except WorkspaceError:
                continue
            if b"\x00" in data[:BINARY_SNIFF_BYTES]:
                continue
            for number, line in enumerate(data.decode("utf-8", errors="replace").splitlines(), start=1):
                if text in line:
                    out.append(f"{rel}:{number}: {line[:MAX_MATCH_CHARS]}")
                    if len(out) >= MAX_SEARCH_MATCHES:
                        out.append(f"[... more than {MAX_SEARCH_MATCHES} matches; search a directory inside ...]")
                        return out
        return out


def remove_tree(root: Path) -> None:
    """Removes a directory the toolchain wrote into, read-only directories included, without following a link."""
    if not root.exists() and not root.is_symlink():
        return
    with contextlib.suppress(OSError):
        _unlock(os.open(root, _DIR_FLAGS))
    shutil.rmtree(root, ignore_errors=True)


def _unlock(fd: int) -> None:
    """Makes every directory under fd writable to its owner, so it can be emptied; fd is closed."""
    try:
        os.fchmod(fd, OWNED_DIR_MODE)
        with os.scandir(fd) as it:
            names = [e.name for e in it if e.is_dir(follow_symlinks=False)]
        for name in names:
            with contextlib.suppress(OSError):
                _unlock(os.open(name, _DIR_FLAGS, dir_fd=fd))
    finally:
        os.close(fd)
