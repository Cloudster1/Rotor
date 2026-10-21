"""Keeps a run's code nodes out of the runner (#89): a process that is not dumpable cannot be traced, nor its memory,
environment or open files read through /proc, by another process of its user that lacks CAP_SYS_PTRACE."""

from __future__ import annotations

import ctypes

from rotor_runner.config import PR_GET_DUMPABLE, PR_SET_DUMPABLE


class GuardError(RuntimeError):
    """The runner could not make itself unreadable to the code it starts, so it does not start."""


def keep_out() -> None:
    """Makes this process not dumpable before it holds any token; a code node it starts then cannot read it."""
    libc = ctypes.CDLL(None, use_errno=True)
    if libc.prctl(PR_SET_DUMPABLE, 0, 0, 0, 0) != 0 or libc.prctl(PR_GET_DUMPABLE, 0, 0, 0, 0) != 0:
        raise GuardError(
            f"The runner could not keep code nodes out of its memory (errno {ctypes.get_errno()}); "
            "run the sandbox on Linux, where prctl(PR_SET_DUMPABLE) is available."
        )
