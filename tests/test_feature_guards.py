# SPDX-License-Identifier: Apache-2.0
"""Regression tests for initialization and immutable rule API boundaries."""

from __future__ import annotations

import pytest
from backend import main
from fastapi import HTTPException


def test_startup_creates_schema_without_inserting_business_rows(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """Startup creates ORM tables and does not open a data-seeding session."""
    created: list[object] = []
    monkeypatch.setattr(
        main.Base.metadata, "create_all", lambda engine: created.append(engine)
    )
    main.startup()

    assert created == [main.engine]


def test_rules_get_reads_project_file_without_allowing_writes() -> None:
    """The list route stays readable while write routes are read-only guards."""
    rows = main.list_rules(None, 1, "viewer")
    assert rows
    assert rows[0]["name"]

    with pytest.raises(HTTPException) as create_error:
        main.create_rule(None, None, 1, "owner")
    assert create_error.value.status_code == 405

    with pytest.raises(HTTPException) as update_error:
        main.update_rule(1, {}, None, 1, "owner")
    assert update_error.value.status_code == 405

    with pytest.raises(HTTPException) as delete_error:
        main.delete_rule(1, None, 1, "owner")
    assert delete_error.value.status_code == 405


def test_content_status_database_constraint_is_registered() -> None:
    """ORM metadata prevents ambiguous lifecycle values at persistence time."""
    content_table = main.Base.metadata.tables["content"]
    checks = {
        constraint.name: str(constraint.sqltext)
        for constraint in content_table.constraints
        if constraint.name
    }

    assert "ck_content_lifecycle_two_states" in checks
    assert "已发布" in checks["ck_content_lifecycle_two_states"]
    assert "未发布" in checks["ck_content_lifecycle_two_states"]
