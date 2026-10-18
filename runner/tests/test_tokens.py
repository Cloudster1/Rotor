"""The runner's handling of wrapped secrets: redaction here, unwrapping against the stack's OpenBao below."""

from __future__ import annotations

import os

import httpx
import pytest

from rotor_runner.config import REDACTED
from rotor_runner.tokens import Redactor, UnwrapError, Unwrapper

# The stack's OpenBao, and the development root token that wraps a secret as the control plane would.
OPENBAO, ROOT = os.environ.get("ROTOR_TEST_OPENBAO_URL", ""), os.environ.get("ROTOR_TEST_OPENBAO_ROOT_TOKEN", "")


def test_the_redactor_cuts_every_secret_it_holds_and_nothing_else() -> None:
    redactor = Redactor()
    redactor.hold("stub_oat_0123456789abcdef")
    redactor.hold("short")
    text = '{"log": "called with Bearer stub_oat_0123456789abcdef", "note": "short words stay"}'
    assert redactor.scrub(text) == f'{{"log": "called with Bearer {REDACTED}", "note": "short words stay"}}'


@pytest.mark.stack
@pytest.mark.anyio
async def test_a_wrapped_token_opens_once_and_never_again() -> None:
    if not (OPENBAO and ROOT):
        pytest.skip("run by make test-runner, inside the stack's network")
    async with httpx.AsyncClient(timeout=10) as client:
        wrapped = await client.post(
            f"{OPENBAO}/v1/sys/wrapping/wrap",
            headers={"X-Vault-Token": ROOT, "X-Vault-Wrap-TTL": "2m"},
            json={"access_token": "stub_oat_wrapped-for-the-test"},
        )
        assert wrapped.status_code == httpx.codes.OK, wrapped.text
        token = wrapped.json()["wrap_info"]["token"]
        unwrapper = Unwrapper(OPENBAO, client)
        assert (await unwrapper.unwrap(token))["access_token"] == "stub_oat_wrapped-for-the-test"
        with pytest.raises(UnwrapError):
            await unwrapper.unwrap(token)
        with pytest.raises(UnwrapError):
            await unwrapper.unwrap("not-a-wrapping-token")
