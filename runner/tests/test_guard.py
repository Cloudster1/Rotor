"""A code node cannot read the runner it runs under: not its environment, memory or open files (#89)."""

from __future__ import annotations

import json
import subprocess
import sys

# What a hostile code node would try, as a child of the runner.
PROBE = """
import json, os
parent, seen = os.getppid(), {}
for name, read in (("environ", lambda p: open(p, "rb").read()), ("mem", lambda p: open(p, "rb")),
                   ("fd", os.listdir)):
    try:
        read(f"/proc/{parent}/{name}")
        seen[name] = "read"
    except PermissionError:
        seen[name] = "denied"
    except OSError as err:
        seen[name] = type(err).__name__
print(json.dumps(seen))
"""

# The runner's part: guard itself or not, hold a secret in its environment, start the probe as it starts a code node.
RUNNER = """
import os, subprocess, sys
from rotor_runner.guard import keep_out
if sys.argv[1] == "guarded":
    keep_out()
child = subprocess.run([sys.executable, "-I", "-c", sys.argv[2]], capture_output=True, text=True, env={})
print(child.stdout, end="")
"""


def probe(mode: str) -> dict[str, str]:
    env = {"ROTOR_RUNNER_BOOTSTRAP": "bootstrap-secret", "PATH": "/usr/bin:/bin"}
    # Only this file's own two scripts run.
    out = subprocess.run(  # noqa: S603
        [sys.executable, "-c", RUNNER, mode, PROBE], capture_output=True, text=True, env=env, check=True
    )
    return dict(json.loads(out.stdout))


def test_a_code_node_cannot_read_the_guarded_runner() -> None:
    assert probe("guarded") == {"environ": "denied", "mem": "denied", "fd": "denied"}


def test_the_probe_reads_a_runner_that_is_not_guarded() -> None:
    # Without the guard the same user reads the environment the runner started with, bootstrap token included.
    assert probe("open")["environ"] == "read"
