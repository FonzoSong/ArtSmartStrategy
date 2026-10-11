# SPDX-License-Identifier: Apache-2.0
"""Tests for bounded migration retries and fail-fast migration errors."""

from __future__ import annotations

from collections.abc import Callable

import pytest
from backend.container_entrypoint import run_migrations_with_retry
from sqlalchemy.exc import OperationalError


def test_migration_retries_transient_connection_errors() -> None:
    """A connection refusal is retried with bounded exponential backoff."""
    attempts = 0
    delays: list[float] = []

    def upgrade() -> None:
        nonlocal attempts
        attempts += 1
        if attempts < 3:
            raise OperationalError("connect", {}, OSError("refused"))

    run_migrations_with_retry(upgrade, delays.append, max_attempts=4)

    assert attempts == 3
    assert delays == [1.0, 2.0]


def test_migration_fails_after_retry_limit() -> None:
    """An unavailable database produces an explicit fatal startup error."""
    attempts = 0
    delays: list[float] = []

    def upgrade() -> None:
        nonlocal attempts
        attempts += 1
        raise OperationalError("connect", {}, OSError("refused"))

    with pytest.raises(RuntimeError, match="after 3 migration attempts"):
        run_migrations_with_retry(upgrade, delays.append, max_attempts=3)

    assert attempts == 3
    assert delays == [1.0, 2.0]


def test_migration_does_not_retry_non_connection_errors() -> None:
    """Migration/schema errors propagate immediately without retrying."""
    attempts = 0

    def upgrade() -> None:
        nonlocal attempts
        attempts += 1
        raise ValueError("broken migration")

    def unexpected_sleep(_: float) -> None:
        pytest.fail("non-connection migration errors must not be retried")

    upgrade_fn: Callable[[], None] = upgrade
    with pytest.raises(ValueError, match="broken migration"):
        run_migrations_with_retry(upgrade_fn, unexpected_sleep, max_attempts=4)

    assert attempts == 1


def test_retry_limit_must_be_positive() -> None:
    """Invalid retry configuration is rejected before running migrations."""
    with pytest.raises(ValueError, match="at least one"):
        run_migrations_with_retry(max_attempts=0)
