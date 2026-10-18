"""Async tests run on asyncio, through anyio's pytest plugin."""

import pytest


@pytest.fixture
def anyio_backend() -> str:
    return "asyncio"
