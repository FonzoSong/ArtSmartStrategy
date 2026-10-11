# SPDX-License-Identifier: Apache-2.0
"""Run Alembic once with bounded retries for transient PostgreSQL failures."""

from __future__ import annotations

import logging
import os
import sys
import time
from collections.abc import Callable, Sequence
from pathlib import Path

from alembic import command
from alembic.config import Config
from sqlalchemy.exc import OperationalError

from backend.settings import ROOT

LOGGER = logging.getLogger(__name__)
MAX_ATTEMPTS = 8
INITIAL_RETRY_SECONDS = 1.0
MAX_RETRY_SECONDS = 5.0


def _upgrade_head() -> None:
    """Apply all pending migrations to the configured database."""
    config = Config(str(ROOT / "alembic.ini"))
    config.set_main_option("script_location", str(ROOT / "migrations"))
    command.upgrade(config, "head")


def run_migrations_with_retry(
    upgrade: Callable[[], None] = _upgrade_head,
    sleep: Callable[[float], None] = time.sleep,
    max_attempts: int = MAX_ATTEMPTS,
) -> None:
    """Retry Alembic only when PostgreSQL reports a connection failure.

    Args:
        upgrade: Callable that performs the migration once.
        sleep: Delay function, replaceable for deterministic tests.
        max_attempts: Maximum number of attempts before startup fails.

    Raises:
        ValueError: If ``max_attempts`` is less than one.
        RuntimeError: If PostgreSQL remains unavailable after all attempts.
        sqlalchemy.exc.SQLAlchemyError: For non-connection migration errors.
    """
    if max_attempts < 1:
        raise ValueError("max_attempts must be at least one")

    for attempt in range(1, max_attempts + 1):
        try:
            upgrade()
            LOGGER.info("PostgreSQL schema migration completed")
            return
        except OperationalError as exc:
            if attempt == max_attempts:
                LOGGER.exception(
                    "PostgreSQL connection failed on migration attempt %s/%s",
                    attempt,
                    max_attempts,
                )
                raise RuntimeError(
                    "PostgreSQL was not reachable after "
                    f"{max_attempts} migration attempts"
                ) from exc

            delay = min(
                INITIAL_RETRY_SECONDS * (2 ** (attempt - 1)),
                MAX_RETRY_SECONDS,
            )
            LOGGER.warning(
                "PostgreSQL connection failed on migration attempt %s/%s; "
                "retrying in %.1f seconds: %s",
                attempt,
                max_attempts,
                delay,
                exc.orig,
            )
            sleep(delay)


def _seed_data() -> None:
    """Inject opt-in fixture data after the schema migration succeeds."""
    import subprocess
    import sys

    subprocess.run(
        [sys.executable, "scripts/seed_data.py", "--force"],
        check=True,
        cwd=Path(__file__).resolve().parents[1],
    )


def main(argv: Sequence[str] | None = None) -> None:
    """Run migration, optional fixture injection, and/or the ASGI server.

    Args:
        argv: Optional command arguments. Supports ``migrate``, ``serve``,
            and ``all``; defaults to ``all`` for direct Docker compatibility.

    Raises:
        ValueError: If an unknown command is supplied.
    """
    args = list(sys.argv[1:] if argv is None else argv)
    mode = args[0] if args else "all"
    if len(args) > 1 or mode not in {"migrate", "serve", "all"}:
        raise ValueError("Usage: container_entrypoint [migrate|serve|all]")
    logging.basicConfig(
        level=logging.INFO,
        format="%(levelname)s %(name)s: %(message)s",
    )
    if mode in {"migrate", "all"}:
        run_migrations_with_retry()
        if os.getenv("SEED_DATA", "false").strip().lower() == "true":
            _seed_data()
    if mode in {"serve", "all"}:
        os.execvpe(
            sys.executable,
            [
                sys.executable,
                "-m",
                "uvicorn",
                "backend.main:app",
                "--host",
                "0.0.0.0",
                "--port",
                "8000",
            ],
            os.environ,
        )


if __name__ == "__main__":
    main()
