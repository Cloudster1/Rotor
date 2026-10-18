"""What the control plane hands a run arrives wrapped: each token and key is in a single-use OpenBao wrapping token
that the runner opens itself, so the secret never travels in clear, and the runner cuts every secret it holds from
what it sends back."""

from __future__ import annotations

from typing import Any

import httpx

from rotor_runner.config import MIN_SECRET_CHARS, REDACTED, UNWRAP_TIMEOUT_S

UNWRAP_PATH = "/v1/sys/wrapping/unwrap"


class UnwrapError(Exception):
    """OpenBao would not open a wrapped secret: used already, expired, or never one."""


class Unwrapper:
    """Opens wrapping tokens at OpenBao; the runner holds no credential there but the wrapping token itself."""

    def __init__(self, openbao: str, client: httpx.AsyncClient | None = None) -> None:
        self._url = openbao.rstrip("/") + UNWRAP_PATH
        self._client = client

    async def unwrap(self, wrapping_token: str) -> dict[str, Any]:
        client = self._client or httpx.AsyncClient(timeout=UNWRAP_TIMEOUT_S)
        try:
            response = await client.post(self._url, headers={"X-Vault-Token": wrapping_token})
        except httpx.HTTPError as err:
            raise UnwrapError(f"OpenBao did not answer at {self._url}: {err}") from err
        finally:
            if self._client is None:
                await client.aclose()
        if response.status_code != httpx.codes.OK:
            raise UnwrapError(
                f"OpenBao refused to unwrap the secret ({response.status_code}): used, expired or not one"
            )
        data = response.json().get("data")
        if not isinstance(data, dict):
            raise UnwrapError("OpenBao unwrapped nothing")
        return data


class Redactor:
    """Every secret the runner holds, to cut out of each event before it leaves the sandbox."""

    def __init__(self) -> None:
        self._secrets: set[str] = set()

    def hold(self, secret: str) -> None:
        if len(secret) >= MIN_SECRET_CHARS:
            self._secrets.add(secret)

    def scrub(self, text: str) -> str:
        for secret in self._secrets:
            text = text.replace(secret, REDACTED)
        return text
