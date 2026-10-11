# SPDX-License-Identifier: Apache-2.0
"""Tests for bounded PostgreSQL readiness checks during app startup."""

from __future__ import annotations

from collections.abc import Callable

import pytest
from backend.database import verify_database_ready
from sqlalchemy.exc import OperationalError


def test_database_startup_retries_transient_connection_error() -> None:
    """A short outage recovers without rerunning Alembic migrations."""
    attempts = 0
    delays: list[float] = []

    def verify() -> None:
        nonlocal attempts
        attempts += 1
        if attempts < 3:
            raise OperationalError("connect", {}, OSError("refused"))

    verify_fn: Callable[[], None] = verify
    verify_database_ready(verify_fn, delays.append, max_attempts=3)

    assert attempts == 3
    assert delays == [1.0, 2.0]


def test_database_startup_stops_after_attempt_limit() -> None:
    """An unavailable database fails startup after a finite wait."""
    attempts = 0
    delays: list[float] = []

    def verify() -> None:
        nonlocal attempts
        attempts += 1
        raise OperationalError("connect", {}, OSError("refused"))

    verify_fn: Callable[[], None] = verify
    with pytest.raises(OperationalError):
        verify_database_ready(verify_fn, delays.append, max_attempts=2)

    assert attempts == 2
    assert delays == [1.0]
