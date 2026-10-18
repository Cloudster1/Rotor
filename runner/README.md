# Rotor runner

The Python program that runs one workflow inside its sandbox. It compiles the
workflow into a LangGraph graph (#16), talks to the control plane over gRPC
(#18) and calls the tools through their MCP servers with the run's
short-lived tokens. The image is distroless, runs as non-root and is tested
with a read-only root filesystem, no network and no capabilities.

```sh
make check-python    # lint, types, tests and a smoke run of the image
make py-fmt          # format and fix in place
make uv-lock         # update uv.lock after changing pyproject.toml
```
