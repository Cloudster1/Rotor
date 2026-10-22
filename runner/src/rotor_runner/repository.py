"""A repository binding's clone address and token, for the runner's own git alone (#137): the token never reaches a
code node or a toolchain container, and the redactor cuts it from all the runner sends, as any binding's."""

from __future__ import annotations

from collections.abc import Mapping
from dataclasses import dataclass, field

from rotor_runner.mcp import Binding, TokenProvider, ToolRefusedError


@dataclass(frozen=True)
class Repository:
    """A project's repository a binding names, with the run's inputs filled in, at the tool's git proxy."""

    project: str
    # The repository's owner/name, as the tool lists it.
    name: str
    url: str


@dataclass(frozen=True)
class RepositoryAccess:
    """What the runner clones and pushes with: the address, and the binding's current token for it."""

    binding: str
    repository: Repository
    # Kept out of repr, so an error or a log line that prints the access never prints the token.
    token: str = field(repr=False)
    writes: bool = False

    @property
    def url(self) -> str:
        return self.repository.url

    def headers(self) -> dict[str, str]:
        """The header git sends to the proxy, which takes the token as a bearer."""
        return {"Authorization": f"Bearer {self.token}"}


class Repositories:
    """The run's repository bindings; a token is asked for at each access, so a long step never holds a stale one."""

    def __init__(self, bindings: Mapping[str, Binding], tokens: TokenProvider) -> None:
        self._bindings, self._tokens = bindings, tokens

    def names(self) -> list[str]:
        """The run's repository bindings, by name."""
        return sorted(name for name, b in self._bindings.items() if b.repository is not None)

    async def access(self, binding: str) -> RepositoryAccess:
        b = self._bindings.get(binding)
        if b is None or b.repository is None:
            raise ToolRefusedError(f"{binding} is not a repository binding of the workflow")
        token = await self._tokens.token(binding)
        return RepositoryAccess(binding=binding, repository=b.repository, token=token, writes=b.writes)
