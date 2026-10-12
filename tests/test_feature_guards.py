# SPDX-License-Identifier: Apache-2.0
"""Regression tests for initialization and immutable rule API boundaries."""

from __future__ import annotations

import pytest
from backend import main
from fastapi import HTTPException


def test_startup_only_verifies_schema_and_does_not_run_migrations(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Only the dedicated migration entrypoint may execute Alembic upgrades."""
    calls: list[str] = []
    monkeypatch.setattr(main, "DATABASE_CONFIGURED", True)
    monkeypatch.setattr(
        main, "verify_database_ready", lambda: calls.append("verify")
    )
    monkeypatch.setattr(main, "use_database_mode", lambda: None)
    main.startup()

    assert calls == ["verify"]


def test_startup_uses_local_example_data_when_database_is_not_configured(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """An absent database URL does not prevent the web app from starting."""
    calls: list[str] = []
    monkeypatch.setattr(main, "DATABASE_CONFIGURED", False)
    monkeypatch.setattr(
        main, "use_example_mode", lambda: calls.append("example")
    )

    main.startup()

    assert calls == ["example"]


def test_startup_falls_back_to_example_data_when_postgres_is_unreachable(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A connection error selects local JSON-backed demo data."""
    from sqlalchemy.exc import OperationalError

    calls: list[str] = []
    monkeypatch.setattr(main, "DATABASE_CONFIGURED", True)
    monkeypatch.setattr(
        main,
        "verify_database_ready",
        lambda: (_ for _ in ()).throw(
            OperationalError("connect", {}, OSError("connection refused"))
        ),
    )
    monkeypatch.setattr(
        main, "use_example_mode", lambda: calls.append("example")
    )

    main.startup()

    assert calls == ["example"]


def test_startup_refuses_to_serve_an_unmigrated_database(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Schema errors propagate instead of starting in example mode."""
    from backend.database import DatabaseSchemaError

    monkeypatch.setattr(main, "DATABASE_CONFIGURED", True)
    monkeypatch.setattr(
        main,
        "verify_database_ready",
        lambda: (_ for _ in ()).throw(DatabaseSchemaError("migration missing")),
    )
    monkeypatch.setattr(
        main,
        "use_example_mode",
        lambda: pytest.fail("unmigrated database must not start example mode"),
    )

    with pytest.raises(DatabaseSchemaError, match="migration missing"):
        main.startup()


def test_rules_are_team_scoped_and_editable():
    """Managed rules support CRUD and reject duplicate active priorities."""
    from backend.database import Base
    from backend.schemas import RuleIn
    from sqlalchemy import create_engine
    from sqlalchemy.orm import Session

    engine = create_engine("sqlite+pysqlite:///:memory:")
    Base.metadata.create_all(engine)
    with Session(engine) as db:
        rows = main.list_rules(db, 1, "viewer")
        assert rows == []
        claims = {"user_id": 1, "name": "规则维护者"}
        payload = RuleIn(
            name="标题长度检查",
            rule_type="length",
            rule_content={"min": 1, "max": 120},
            priority=20,
        )
        created = main.create_rule(payload, db, 1, claims, "owner")
        assert created["name"] == "标题长度检查"
        assert created["rule_content"] == {"min": 1, "max": 120}
        assert main.list_rules(db, 1, "viewer")[0]["id"] == created["id"]
        with pytest.raises(HTTPException) as conflict:
            main.create_rule(payload, db, 1, claims, "owner")
        assert conflict.value.status_code == 409
        updated = main.update_rule(
            created["id"], {"enabled": False}, db, 1, claims, "owner"
        )
        assert updated["enabled"] is False
        assert main.delete_rule(created["id"], db, 1, claims, "owner") == {
            "deleted": True
        }


def test_content_status_database_constraint_is_registered() -> None:
    """ORM metadata prevents ambiguous lifecycle values at persistence time."""
    from backend.database import Base

    content_table = Base.metadata.tables["content"]
    checks = {
        constraint.name: str(constraint.sqltext)
        for constraint in content_table.constraints
        if constraint.name
    }

    assert "ck_content_lifecycle_two_states" in checks
    assert "已发布" in checks["ck_content_lifecycle_two_states"]
    assert "未发布" in checks["ck_content_lifecycle_two_states"]
