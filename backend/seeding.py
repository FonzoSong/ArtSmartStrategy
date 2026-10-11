# SPDX-License-Identifier: Apache-2.0
"""Idempotent injection of the checked-in test-data fixture."""

from __future__ import annotations

import json
from collections.abc import Callable
from datetime import datetime
from pathlib import Path
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from backend.models import Content, Team
from backend.settings import ROOT

FIXTURE_PATH = ROOT / "tests" / "fixtures" / "content_seed.json"
SEED_SOURCE = "test-seed-v0.4"


def load_seed_fixture(path: Path = FIXTURE_PATH) -> list[dict[str, Any]]:
    """Read the versioned seed fixture from UTF-8 JSON.

    Args:
        path: Fixture path, injectable for tests.

    Returns:
        Parsed fixture rows.
    """
    values = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(values, list) or any(
        not isinstance(item, dict) for item in values
    ):
        raise ValueError("测试数据 fixture 必须是 JSON 对象数组")
    return values


def confirm_seed_over_existing(
    db: Session,
    team_id: int,
    force: bool,
    confirm: Callable[[str], str],
) -> bool:
    """Ask before mixing a fixture into an existing non-seed content set.

    Args:
        db: Active database session.
        team_id: Team that would receive test rows.
        force: Whether explicit force mode bypasses confirmation.
        confirm: Input callback used to request confirmation.

    Returns:
        Whether fixture injection may proceed.
    """
    if force:
        return True
    has_seed = (
        db.scalar(
            select(Content.id)
            .where(Content.team_id == team_id, Content.source == SEED_SOURCE)
            .limit(1)
        )
        is not None
    )
    has_content = (
        db.scalar(select(Content.id).where(Content.team_id == team_id).limit(1))
        is not None
    )
    if not has_content or has_seed:
        return True
    response = confirm("团队已有内容数据。仍要注入测试数据吗？(y/N): ")
    return response.strip().lower() in {"y", "yes"}


def seed_team(db: Session, team_id: int, fixture: list[dict[str, Any]]) -> int:
    """Insert fixture rows missing from the selected team.

    Args:
        db: Active transaction session.
        team_id: Existing team receiving the test content.
        fixture: Validated fixture rows.

    Returns:
        Number of records newly inserted.
    """
    if db.get(Team, team_id) is None:
        has_teams = db.scalar(select(Team.id).limit(1)) is not None
        if has_teams or team_id != 1:
            raise ValueError(f"团队 {team_id} 不存在")
        db.add(Team(id=1, name="测试数据团队"))
        db.flush()
    inserted = 0
    for row in fixture:
        exists = db.scalar(
            select(Content.id).where(
                Content.team_id == team_id,
                Content.source == SEED_SOURCE,
                Content.title == row["title"],
            )
        )
        if exists:
            continue
        likes = int(row["likes"])
        comments = int(row["comments"])
        saves = int(row["saves"])
        shares = int(row["shares"])
        db.add(
            Content(
                team_id=team_id,
                title=row["title"],
                category=row["category"],
                platform=row["platform"],
                published_at=datetime.fromisoformat(row["published_at"]),
                duration=row["duration"],
                views=row["views"],
                likes=likes,
                comments=comments,
                saves=saves,
                shares=shares,
                completion=row["completion"],
                total_interactions=likes + comments + saves + shares,
                tags_json=json.dumps(row["tags"], ensure_ascii=False),
                source=SEED_SOURCE,
                lifecycle="已发布",
                comment_text=row["comment_text"],
            )
        )
        inserted += 1
    db.commit()
    return inserted
