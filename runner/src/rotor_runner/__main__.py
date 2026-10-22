"""Entry point of the sandbox image: the runner takes its run from the control plane, or checks itself offline."""

import argparse
import asyncio
import logging
import os
import sys

from rotor_runner.config import LOG_FORMAT
from rotor_runner.graph import SELF_CHECK_ANSWER, self_check
from rotor_runner.guard import keep_out
from rotor_runner.redact import RedactingFormatter, Redactor

EXIT_USAGE = 2
# What the sandbox is started with, read once and removed, so nothing the runner starts can find them.
ENV_ADDRESS, ENV_BOOTSTRAP, ENV_CA = "ROTOR_CONTROL_PLANE_ADDR", "ROTOR_RUNNER_BOOTSTRAP", "ROTOR_RUNNER_CA"
ENV_OPENBAO = "ROTOR_OPENBAO_ADDR"
# The sandbox's toolchains and the secret their agents take (#136), set only for a run with workspace steps.
ENV_WORKSPACE, ENV_WORKSPACE_SECRET = "ROTOR_WORKSPACE", "ROTOR_WORKSPACE_SECRET"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="rotor-runner", description="Runs a Rotor workflow inside its sandbox.")
    parser.add_argument("--self-check", action="store_true", help="build and run a graph offline, then exit")
    parser.add_argument("--workspace-check", action="store_true", help="prove the sandbox's toolchains, then exit")
    args = parser.parse_args(argv)
    # Before the bootstrap token is read: from here on no code node can read this process or its environment. The
    # self-check runs it too, so the image is proven able to.
    keep_out()
    if args.self_check:
        answer = self_check()
        print(f"rotor-runner self-check: {answer}")
        return 0 if answer == SELF_CHECK_ANSWER else 1
    if args.workspace_check:
        from rotor_runner.workspace import WorkspaceConfig
        from rotor_runner.workspace.check import run_check

        return run_check(
            WorkspaceConfig.from_env(*(os.environ.pop(k, "") for k in (ENV_WORKSPACE, ENV_WORKSPACE_SECRET)))
        )
    address, bootstrap, ca, openbao, told, secret = (
        os.environ.pop(key, "")
        for key in (ENV_ADDRESS, ENV_BOOTSTRAP, ENV_CA, ENV_OPENBAO, ENV_WORKSPACE, ENV_WORKSPACE_SECRET)
    )
    if not (address and bootstrap and ca and openbao):
        parser.print_usage(sys.stderr)
        print(
            f"Set {ENV_ADDRESS}, {ENV_BOOTSTRAP}, {ENV_CA} and {ENV_OPENBAO}, as the control plane does for a sandbox.",
            file=sys.stderr,
        )
        return EXIT_USAGE
    # One redactor holds every secret the run is handed, so neither its events nor its log lines carry one.
    redactor = Redactor()
    handler = logging.StreamHandler()
    handler.setFormatter(RedactingFormatter(redactor, LOG_FORMAT))
    logging.basicConfig(level=logging.INFO, handlers=[handler])
    from rotor_runner.control import Channel
    from rotor_runner.workspace import WorkspaceConfig

    if secret:
        redactor.hold(secret)
    channel = Channel(address, ca.encode(), bootstrap, openbao, redactor)
    channel.workspace = WorkspaceConfig.from_env(told, secret)
    return asyncio.run(channel.run())


if __name__ == "__main__":
    sys.exit(main())
