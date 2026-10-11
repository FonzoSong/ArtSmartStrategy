# SPDX-License-Identifier: Apache-2.0
"""SQLAlchemy base, PostgreSQL engine, and request-scoped sessions."""

from __future__ import annotations

import json
import logging
import time
from collections.abc import Callable, Iterator
from datetime import datetime
from pathlib import Path

from sqlalchemy import create_engine, inspect
from sqlalchemy.exc import OperationalError, SQLAlchemyError
from sqlalchemy.orm import DeclarativeBase, Session, sessionmaker
from sqlalchemy.pool import StaticPool

from backend.example_data import load_example_data
from backend.security import password_hash
from backend.settings import DATABASE_URL, ROOT

logger = logging.getLogger("uvicorn.error")


class Base(DeclarativeBase):
    """Base class for SQLAlchemy models."""


engine = create_engine(
    DATABASE_URL,
    future=True,
    pool_pre_ping=True,
    connect_args={"connect_timeout": 5},
)
SessionLocal = sessionmaker(
    bind=engine, autoflush=False, expire_on_commit=False
)
DEMO_MODE = False
_demo_engine = None


class DatabaseSchemaError(RuntimeError):
    """Raised when the database is reachable but its schema is not current."""


def verify_database_schema() -> None:
    """Verify PostgreSQL connectivity, tables, and Alembic head revisions.

    Raises:
        DatabaseSchemaError: If required tables or migrations are missing.
        sqlalchemy.exc.SQLAlchemyError: If PostgreSQL cannot be reached.
    """
    from alembic.config import Config
    from alembic.migration import MigrationContext
    from alembic.script import ScriptDirectory

    from backend import models  # noqa: F401

    with engine.connect() as connection:
        table_names = set(inspect(connection).get_table_names())
        required_tables = set(Base.metadata.tables)
        missing_tables = sorted(required_tables - table_names)
        if missing_tables:
            raise DatabaseSchemaError(
                "数据库缺少应用表：" + ", ".join(missing_tables)
            )

        config = Config(str(ROOT / "alembic.ini"))
        config.set_main_option("script_location", str(ROOT / "migrations"))
        expected_heads = set(ScriptDirectory.from_config(config).get_heads())
        current_heads = set(
            MigrationContext.configure(connection).get_current_heads()
        )
        if current_heads != expected_heads:
            raise DatabaseSchemaError(
                "数据库迁移版本不匹配；当前="
                f"{sorted(current_heads)}，期望={sorted(expected_heads)}。"
                "请先运行 backend.container_entrypoint"
            )


def verify_database_ready(
    verify: Callable[[], None] = verify_database_schema,
    sleep: Callable[[float], None] = time.sleep,
    max_attempts: int = 3,
) -> None:
    """Retry transient PostgreSQL connection failures during app startup.

    Args:
        verify: Schema and connection verification callable.
        sleep: Delay function, replaceable for deterministic tests.
        max_attempts: Maximum number of verification attempts.

    Raises:
        ValueError: If ``max_attempts`` is less than one.
        sqlalchemy.exc.OperationalError: If PostgreSQL remains unavailable.
        DatabaseSchemaError: If required tables or migrations are missing.
    """
    if max_attempts < 1:
        raise ValueError("max_attempts must be at least one")

    for attempt in range(1, max_attempts + 1):
        try:
            verify()
            return
        except OperationalError as exc:
            if attempt == max_attempts:
                logger.exception(
                    "PostgreSQL startup connection failed after %s attempts",
                    attempt,
                )
                raise
            delay = float(2 ** (attempt - 1))
            logger.warning(
                "PostgreSQL startup connection failed on attempt %s/%s; "
                "retrying in %.1f seconds: %s",
                attempt,
                max_attempts,
                delay,
                exc.orig,
            )
            sleep(delay)


def use_example_mode() -> None:
    """Initialize an ephemeral database from validated external example data.

    The in-memory store is process-local and never writes to PostgreSQL.
    """
    global DEMO_MODE, SessionLocal, _demo_engine
    from backend.content import sync_tag_links
    from backend.models import (
        Content,
        PerformanceBooking,
        Schedule,
        Team,
        User,
    )

    example_path = Path(__file__).resolve().parents[1] / "example" / "data.json"
    try:
        payload = load_example_data()
        teams = payload.get("teams") or [payload["team"]]
        _demo_engine = create_engine(
            "sqlite+pysqlite:///:memory:",
            connect_args={"check_same_thread": False},
            poolclass=StaticPool,
        )
        Base.metadata.create_all(_demo_engine)
        SessionLocal = sessionmaker(
            bind=_demo_engine, autoflush=False, expire_on_commit=False
        )
        with SessionLocal() as db:
            team_by_id = {int(row["id"]): row for row in teams}
            for row in teams:
                db.add(Team(id=int(row["id"]), name=row["name"]))
            db.flush()
            users_by_email: dict[str, User] = {}
            for row in payload["users"]:
                user_id = int(row.get("id", len(users_by_email) + 11))
                user_team = int(row.get("team_id", payload["team"]["id"]))
                person = User(
                    id=user_id,
                    team_id=user_team,
                    email=row["email"],
                    name=row["name"],
                    role_name=row["role"],
                    password_hash=password_hash(payload["demo_password"]),
                )
                db.add(person)
                users_by_email[row["email"]] = person
            db.flush()
            for row in payload["performances"]:
                user = users_by_email[row["owner_email"]]
                team_id = int(row.get("team_id", user.team_id))
                if team_id not in team_by_id:
                    raise ValueError(f"演出 {row['id']} 引用了未知团队")
                db.add(
                    PerformanceBooking(
                        id=row["id"],
                        team_id=team_id,
                        title=row["title"],
                        venue=row.get("venue", ""),
                        starts_at=datetime.fromisoformat(
                            row["starts_at"]
                        ).replace(tzinfo=None),
                        booking_status=row.get("booking_status", "待确认"),
                        owner_user_id=user.id,
                        notes=row.get("notes", ""),
                    )
                )
            db.flush()
            for row in payload["contents"]:
                user = users_by_email[row["owner_email"]]
                team_id = int(row.get("team_id", user.team_id))
                item = Content(
                    id=row["id"],
                    team_id=team_id,
                    title=row["title"],
                    category=row["category"],
                    platform=row["platform"],
                    published_at=datetime.fromisoformat(
                        row["published_at"]
                    ).replace(tzinfo=None),
                    duration=row.get("duration", 0),
                    views=row.get("views", 0),
                    likes=row.get("likes", 0),
                    comments=row.get("comments", 0),
                    saves=row.get("saves", 0),
                    shares=row.get("shares", 0),
                    completion=row.get("completion", 0),
                    total_interactions=sum(
                        row.get(k, 0)
                        for k in ("likes", "comments", "saves", "shares")
                    ),
                    lifecycle=row["lifecycle"],
                    owner=user.name,
                    performance_id=row.get("performance_id"),
                    owner_user_id=user.id,
                    workflow_state=row.get("workflow_state", "待制作"),
                    tags_json=json.dumps(
                        row.get("tags", []), ensure_ascii=False
                    ),
                    materials=row.get("materials", ""),
                    cover_url=row.get("cover_url", ""),
                    notes=row.get("notes", ""),
                    source="example",
                )
                db.add(item)
                db.flush()
                sync_tag_links(db, item, row.get("tags", []), user.id)
            for row in payload.get("schedules", []):
                content = db.get(Content, row["content_id"])
                if content is None:
                    raise ValueError(
                        f"示例排期关联的内容不存在: {row['content_id']}"
                    )
                user = users_by_email[row["owner_email"]]
                db.add(
                    Schedule(
                        team_id=content.team_id,
                        content_id=content.id,
                        planned_date=datetime.fromisoformat(
                            row["planned_date"]
                        ).date(),
                        planned_time=row["planned_time"],
                        title=content.title,
                        category=content.category,
                        platform=content.platform,
                        owner=user.name,
                        source=row["source"],
                        created_by_user_id=user.id,
                        status="待发布",
                    )
                )
            db.commit()
        DEMO_MODE = True
        logger.warning(
            "PostgreSQL unavailable; using process-local example mode from %s. "
            "Changes will not persist after restart.",
            example_path,
        )
    except (OSError, KeyError, TypeError, ValueError) as exc:
        logger.exception("Unable to load example data at %s", example_path)
        raise RuntimeError(f"示例数据模式无法启动：{exc}") from exc


def runtime_mode() -> str:
    """Return the active persistence mode."""
    return "example" if DEMO_MODE else "database"


def use_database_mode() -> None:
    """Restore the configured PostgreSQL session factory after recovery."""
    global DEMO_MODE, SessionLocal
    was_demo = DEMO_MODE
    SessionLocal = sessionmaker(
        bind=engine, autoflush=False, expire_on_commit=False
    )
    DEMO_MODE = False
    if was_demo:
        logger.info(
            "PostgreSQL recovered; application returned to database mode"
        )


def get_db() -> Iterator[Session]:
    """Yield a database session and close it after the request.

    Yields:
        A SQLAlchemy session connected to the configured PostgreSQL database.
    """
    db = SessionLocal()
    try:
        yield db
    except SQLAlchemyError:
        logger.exception(
            "Database request failed in %s mode; data mode will not switch "
            "automatically and no fallback database will receive the write",
            runtime_mode(),
        )
        raise
    finally:
        db.close()
