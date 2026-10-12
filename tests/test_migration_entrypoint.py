# SPDX-License-Identifier: Apache-2.0
"""Tests for bounded migration retries and fail-fast migration errors."""

from __future__ import annotations

from collections.abc import Callable

import pytest
from backend import container_entrypoint
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


def test_all_mode_starts_app_when_database_connection_is_unavailable(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Compose startup serves the app in local example mode during outages."""
    executed: list[list[str]] = []

    def unavailable_migration(**_: object) -> None:
        cause = OperationalError("connect", {}, OSError("connection refused"))
        raise RuntimeError("PostgreSQL unavailable") from cause

    monkeypatch.setattr(
        container_entrypoint,
        "run_migrations_with_retry",
        unavailable_migration,
    )
    monkeypatch.setattr(
        container_entrypoint.os,
        "execvpe",
        lambda _exe, args, _env: executed.append(list(args)),
    )
    monkeypatch.setenv("SEED_DATA", "false")

    container_entrypoint.main(["all"])

    assert executed
    assert "uvicorn" in executed[0]


def test_all_mode_does_not_hide_a_migration_error(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Schema and migration failures remain fatal rather than using JSON."""
    monkeypatch.setattr(
        container_entrypoint,
        "run_migrations_with_retry",
        lambda **_: (_ for _ in ()).throw(RuntimeError("bad migration")),
    )
    with pytest.raises(RuntimeError, match="bad migration"):
        container_entrypoint.main(["all"])


def test_prepare_mode_returns_success_after_connection_retries(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """The Compose migration job can finish so app fallback can start."""
    calls: list[int] = []

    def unavailable_migration(**kwargs: object) -> None:
        calls.append(int(kwargs["max_attempts"]))
        cause = OperationalError("connect", {}, OSError("connection refused"))
        raise RuntimeError("PostgreSQL unavailable") from cause

    monkeypatch.setattr(
        container_entrypoint,
        "run_migrations_with_retry",
        unavailable_migration,
    )
    monkeypatch.setenv("SEED_DATA", "false")

    container_entrypoint.main(["prepare"])

    assert calls == [3]


def test_prepare_mode_does_not_hide_migration_errors(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A broken migration is still fatal to the one-shot Compose job."""
    monkeypatch.setattr(
        container_entrypoint,
        "run_migrations_with_retry",
        lambda **_: (_ for _ in ()).throw(RuntimeError("bad migration")),
    )
    with pytest.raises(RuntimeError, match="bad migration"):
        container_entrypoint.main(["prepare"])
