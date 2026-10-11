# SPDX-License-Identifier: Apache-2.0
"""Team-isolation tests for performance and content ownership links."""

from __future__ import annotations

from datetime import datetime

import pytest
from backend.database import Base
from backend.main import validate_owner, validate_performance
from backend.models import PerformanceBooking, Team, User
from fastapi import HTTPException
from sqlalchemy import create_engine
from sqlalchemy.orm import Session
from sqlalchemy.pool import StaticPool


@pytest.fixture
def isolated_session() -> Session:
    """Return a schema-isolated session with two independent teams."""
    engine = create_engine(
        "sqlite+pysqlite:///:memory:",
        connect_args={"check_same_thread": False},
        poolclass=StaticPool,
    )
    Base.metadata.create_all(engine)
    session = Session(engine, expire_on_commit=False)
    session.add_all(
        [
            Team(id=101, name="A"),
            Team(id=202, name="B"),
            User(
                id=1011,
                team_id=101,
                email="a@example.invalid",
                name="A",
                password_hash="x",
            ),
            User(
                id=2022,
                team_id=202,
                email="b@example.invalid",
                name="B",
                password_hash="x",
            ),
            PerformanceBooking(
                id="booking-b",
                team_id=202,
                title="演出 B",
                starts_at=datetime(2026, 11, 1),
            ),
        ]
    )
    session.commit()
    try:
        yield session
    finally:
        session.close()
        engine.dispose()


def test_owner_and_performance_references_are_team_scoped(
    isolated_session: Session,
) -> None:
    """Cross-team owners and performance IDs are rejected at the API boundary."""
    assert validate_owner(isolated_session, 101, 1011).name == "A"
    with pytest.raises(HTTPException) as owner_error:
        validate_owner(isolated_session, 101, 2022)
    assert owner_error.value.status_code == 422

    with pytest.raises(HTTPException) as booking_error:
        validate_performance(isolated_session, 101, "booking-b")
    assert booking_error.value.status_code == 422
