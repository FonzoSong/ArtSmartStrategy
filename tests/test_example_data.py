# SPDX-License-Identifier: Apache-2.0
"""Validation and loading tests for external runtime example data."""

from __future__ import annotations

from pathlib import Path

import pytest
from backend import database
from backend.example_data import load_example_data
from backend.models import Content, PerformanceBooking, Team, User
from backend.security import verify_password
from sqlalchemy import select


def test_external_example_data_has_consistent_business_links() -> None:
    """Example records reference known owners and bookings from the JSON file."""
    payload = load_example_data()
    users = {row["email"] for row in payload["users"]}
    performances = {row["id"] for row in payload["performances"]}

    assert len(payload["performances"]) >= 3
    assert len(payload["contents"]) == 120
    assert len(payload["teams"]) >= 2
    for state in {row["workflow_state"] for row in payload["contents"]}:
        assert (
            sum(row["workflow_state"] == state for row in payload["contents"])
            >= 8
        )
    assert {row["source"] for row in payload["schedules"]} == {
        "automatic",
        "manual",
    }
    assert any(row["lifecycle"] == "已发布" for row in payload["contents"])
    assert any(row["lifecycle"] == "未发布" for row in payload["contents"])
    assert all(row["owner_email"] in users for row in payload["contents"])
    assert all(
        row["performance_id"] in performances for row in payload["contents"]
    )


def test_invalid_example_schema_has_actionable_error(tmp_path: Path) -> None:
    """Malformed external JSON is reported without fallback defaults."""
    example = tmp_path / "example"
    example.mkdir()
    (example / "data.json").write_text('{"version": 999}', encoding="utf-8")
    (example / "schema.json").write_text(
        (
            Path(__file__).resolve().parents[1] / "example" / "schema.json"
        ).read_text(encoding="utf-8"),
        encoding="utf-8",
    )

    with pytest.raises(ValueError, match="root:"):
        load_example_data(tmp_path)


def test_example_mode_loads_team_content_owner_and_booking(monkeypatch) -> None:
    """Example mode runs in an ephemeral DB and creates linked read data."""
    previous_factory = database.SessionLocal
    previous_mode = database.DEMO_MODE
    payload = load_example_data()
    try:
        database.use_example_mode()
        assert database.runtime_mode() == "example"
        with database.SessionLocal() as session:
            team = session.scalar(select(Team))
            owner = session.scalar(
                select(User).where(User.role_name == "owner")
            )
            content = session.scalar(
                select(Content).where(Content.lifecycle == "未发布")
            )
            booking = session.get(PerformanceBooking, content.performance_id)
            assert team is not None
            assert owner is not None
            assert verify_password(
                payload["demo_password"], owner.password_hash
            )
            assert booking is not None
            assert content.owner_user_id is not None
            assert content.owner
    finally:
        database.SessionLocal = previous_factory
        database.DEMO_MODE = previous_mode
