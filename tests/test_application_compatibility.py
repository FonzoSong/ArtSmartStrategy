# SPDX-License-Identifier: Apache-2.0
"""Tests for stable application and API entry points."""

import importlib

import pytest
from backend import database, settings
from backend.database import Base, engine
from backend.main import app
from backend.schemas import ContentIn
from fastapi.testclient import TestClient


def test_main_module_keeps_the_existing_fastapi_entry_point() -> None:
    """The established backend.main:app import and API root stay available."""
    response = TestClient(app).get("/api")

    assert response.status_code == 200
    assert response.json() == {
        "name": "艺数智策 API",
        "docs": "/docs",
        "health": "/api/health",
    }


def test_authentication_routes_keep_their_public_paths() -> None:
    """Login and registration URLs remain stable across the refactor."""
    paths = {route.path for route in app.routes}

    assert {
        "/api/auth/setup",
        "/api/auth/register-team",
        "/api/auth/login",
    } <= paths


def test_application_database_engine_is_postgresql() -> None:
    """The app no longer accepts SQLite for request data."""
    assert engine.dialect.name == "postgresql"


def test_configuration_rejects_sqlite_urls(monkeypatch) -> None:
    """SQLite cannot silently become the application database backend."""
    original_url = settings.DATABASE_URL
    monkeypatch.setenv("DATABASE_URL", "sqlite:///forbidden.db")
    try:
        with pytest.raises(RuntimeError, match="必须使用 PostgreSQL"):
            importlib.reload(settings)
    finally:
        monkeypatch.setenv("DATABASE_URL", original_url)
        importlib.reload(settings)


def test_all_orm_tables_are_registered_for_migrations() -> None:
    """Importing the model module registers the complete schema metadata."""
    assert {
        "team",
        "user_account",
        "content",
        "content_rule",
        "rule_execution_log",
        "schedule",
    } <= set(Base.metadata.tables)


def test_database_dependency_closes_the_session(monkeypatch) -> None:
    """Request-scoped sessions close after dependency iteration finishes."""

    class FakeSession:
        """A minimal fake with the lifecycle used by get_db."""

        closed = False

        def close(self) -> None:
            """Record closure of the fake request session."""
            self.closed = True

    session = FakeSession()
    monkeypatch.setattr(database, "SessionLocal", lambda: session)
    assert list(database.get_db()) == [session]
    assert session.closed


def test_content_schema_preserves_the_import_status_default() -> None:
    """Omitted content status defaults to published for import compatibility."""
    content = ContentIn(
        title="示例内容",
        platform="短视频平台A",
        published_at="2026-10-11T12:00:00",
    )

    assert content.lifecycle == "已发布"
    assert content.tags == []


def test_content_schema_normalizes_legacy_status_values() -> None:
    """The request model preserves the earlier status compatibility rules."""
    content = ContentIn(
        title="历史记录",
        platform="短视频平台A",
        published_at="2026-10-11T12:00:00",
        lifecycle="已复盘",
    )

    assert content.lifecycle == "已发布"
