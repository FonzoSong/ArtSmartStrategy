# SPDX-License-Identifier: Apache-2.0
"""Tests for explicit, idempotent fixture injection."""

from __future__ import annotations

from collections.abc import Iterator
from datetime import datetime
from pathlib import Path

import pytest
from backend.database import Base
from backend.models import Content, Team
from backend.seeding import (
    confirm_seed_over_existing,
    load_seed_fixture,
    seed_team,
)
from sqlalchemy import create_engine, select
from sqlalchemy.orm import Session


@pytest.fixture
def session(tmp_path: Path) -> Iterator[Session]:
    """Create a temporary SQLite schema for isolated fixture tests."""
    engine = create_engine(f"sqlite:///{tmp_path / 'test.db'}")
    Base.metadata.create_all(engine)
    with Session(engine) as db:
        db.add(Team(id=1, name="测试团队"))
        db.commit()
        yield db
    engine.dispose()


def test_fixture_injection_is_explicit_and_idempotent(session: Session) -> None:
    """Repeated seed calls only insert each fixture row once."""
    fixture = load_seed_fixture()

    assert session.scalar(select(Content.id)) is None
    assert seed_team(session, 1, fixture) == 3
    assert seed_team(session, 1, fixture) == 0
    rows = session.scalars(select(Content)).all()
    assert len(rows) == 3
    assert {item.source for item in rows} == {"test-seed-v0.4"}


def test_seed_requires_existing_team(session: Session) -> None:
    """A non-default team must exist before it can receive fixture records."""
    with pytest.raises(ValueError, match="团队 99 不存在"):
        seed_team(session, 99, [])


@pytest.mark.parametrize(("answer", "expected"), [("y", True), ("n", False)])
def test_existing_business_data_requires_confirmation(
    session: Session, answer: str, expected: bool
) -> None:
    """Unrelated existing records require a positive confirmation."""
    session.add(
        Content(
            team_id=1,
            title="真实内容",
            category="艺术科普",
            platform="内容社区C",
            published_at=datetime(2026, 10, 1, 12, 0),
            lifecycle="已发布",
        )
    )
    session.commit()
    prompts: list[str] = []

    def confirm(prompt: str) -> str:
        prompts.append(prompt)
        return answer

    assert confirm_seed_over_existing(session, 1, False, confirm) is expected
    assert len(prompts) == 1


def test_force_skips_existing_data_confirmation(session: Session) -> None:
    """Explicit force mode does not invoke the interactive callback."""
    assert confirm_seed_over_existing(
        session, 1, True, lambda _prompt: pytest.fail("unexpected prompt")
    )
