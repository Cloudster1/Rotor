# Rotor

Rotor is the agentic backplane of the suite. People create, start and watch
workflows that read and write [Exciter](https://github.com/Cloudster1/Exciter),
[Armature](https://github.com/Cloudster1/Armature) and
[Stator](https://github.com/praetorianer777/Stator) through their MCP servers.
Workflows are LangGraph graphs that run in short-lived sandboxes, always with
the permissions of the person who started them, and every secret they hold is
short-lived and minted by the tool it opens.

Rotor is at its beginning; the
[milestones](https://github.com/Cloudster1/Rotor/milestones) set out the path.

## Running it

The host needs Docker, make, `jq` and `python3`; every toolchain runs in a
container.

```sh
make up          # the stack, and where each part of it listens
./run-tests.sh   # the whole gate, as CI runs it
make help        # every target
```

[`CONTRIBUTING.md`](CONTRIBUTING.md) explains how to work on Rotor and
[`SECURITY.md`](SECURITY.md) how to report a vulnerability and which
development credentials the stack ships with.

## License

Apache License 2.0; see [`LICENSE`](LICENSE) and [`NOTICE`](NOTICE).
