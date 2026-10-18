"""Entry point of the sandbox image: the runner takes its run from the control plane, or checks itself offline."""

import argparse
import asyncio
import logging
import os
import sys

from rotor_runner.graph import SELF_CHECK_ANSWER, self_check

EXIT_USAGE = 2
# What the sandbox is started with, read once and removed, so nothing the runner starts can find them.
ENV_ADDRESS, ENV_BOOTSTRAP, ENV_CA = "ROTOR_CONTROL_PLANE_ADDR", "ROTOR_RUNNER_BOOTSTRAP", "ROTOR_RUNNER_CA"


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="rotor-runner", description="Runs a Rotor workflow inside its sandbox.")
    parser.add_argument("--self-check", action="store_true", help="build and run a graph offline, then exit")
    args = parser.parse_args(argv)
    if args.self_check:
        answer = self_check()
        print(f"rotor-runner self-check: {answer}")
        return 0 if answer == SELF_CHECK_ANSWER else 1
    address, bootstrap, ca = (os.environ.pop(key, "") for key in (ENV_ADDRESS, ENV_BOOTSTRAP, ENV_CA))
    if not (address and bootstrap and ca):
        parser.print_usage(sys.stderr)
        print(
            f"Set {ENV_ADDRESS}, {ENV_BOOTSTRAP} and {ENV_CA}, as the control plane does for a sandbox.",
            file=sys.stderr,
        )
        return EXIT_USAGE
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    from rotor_runner.control import Channel

    return asyncio.run(Channel(address, ca.encode(), bootstrap).run())


if __name__ == "__main__":
    sys.exit(main())
