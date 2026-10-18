"""Entry point of the sandbox image; the runner channel to the control plane arrives with #18."""

import argparse
import sys

from rotor_runner.graph import SELF_CHECK_ANSWER, self_check

EXIT_USAGE = 2


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="rotor-runner", description="Runs a Rotor workflow inside its sandbox.")
    parser.add_argument("--self-check", action="store_true", help="build and run a graph offline, then exit")
    args = parser.parse_args(argv)
    if not args.self_check:
        parser.print_usage(sys.stderr)
        print("Nothing to run yet: the runner takes its work from the control plane from #18 on.", file=sys.stderr)
        return EXIT_USAGE
    answer = self_check()
    print(f"rotor-runner self-check: {answer}")
    return 0 if answer == SELF_CHECK_ANSWER else 1


if __name__ == "__main__":
    sys.exit(main())
