# SPDX-License-Identifier: Apache-2.0
"""Idempotent injection of the checked-in test-data fixture."""

from __future__ import annotations

import json
import uuid
from collections.abc import Callable
from datetime import date, datetime
from pathlib import Path
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from backend.content import sync_tag_links
from backend.models import (
    Content,
    PerformanceBooking,
    Schedule,
    Team,
    User,
)
from backend.security import password_hash
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


def seed_example_team(
    db: Session,
    team_id: int,
    payload: dict[str, Any],
) -> int:
    """Import the external 120-record example set into one database team.

    Existing team members are reused, while an empty database receives the
    sample members and bookings needed to keep all references valid.

    Args:
        db: Active database session.
        team_id: Existing team receiving the example assets.
        payload: Validated object returned by ``load_example_data``.

    Returns:
        Number of newly created content records.
    """
    if not payload.get("contents") or len(payload["contents"]) != 120:
        raise ValueError("示例数据必须包含 120 条内容记录")
    team = db.get(Team, team_id)
    if team is None:
        has_teams = db.scalar(select(Team.id).limit(1)) is not None
        if has_teams or team_id != 1:
            raise ValueError(f"团队 {team_id} 不存在")
        db.add(Team(id=team_id, name=payload["team"]["name"]))
        db.flush()

    members = db.scalars(
        select(User).where(User.team_id == team_id).order_by(User.id)
    ).all()
    external_users = payload["users"]
    user_map: dict[str, User] = {}
    if members:
        user_map = {
            row["email"]: members[index % len(members)]
            for index, row in enumerate(external_users)
        }
    else:
        for index, row in enumerate(external_users, start=1):
            email = row["email"]
            if db.scalar(select(User.id).where(User.email == email)):
                local, _, domain = email.partition("@")
                email = f"{local}+example{team_id}@{domain}"
            user = User(
                team_id=team_id,
                email=email,
                name=row["name"],
                role_name=row["role"],
                password_hash=password_hash(payload["demo_password"]),
            )
            db.add(user)
            user_map[row["email"]] = user
        db.flush()

    performance_ids: dict[str, str] = {}
    for row in payload["performances"]:
        target_id = str(
            uuid.uuid5(uuid.NAMESPACE_URL, f"arts-seed-{team_id}-{row['id']}")
        )
        performance_ids[row["id"]] = target_id
        if db.get(PerformanceBooking, target_id):
            continue
        owner = user_map[row["owner_email"]]
        db.add(
            PerformanceBooking(
                id=target_id,
                team_id=team_id,
                title=row["title"],
                venue=row.get("venue", ""),
                starts_at=datetime.fromisoformat(row["starts_at"]).replace(
                    tzinfo=None
                ),
                booking_status=row.get("booking_status", "已确认"),
                owner_user_id=owner.id,
                notes=row.get("notes", ""),
            )
        )
    db.flush()

    content_ids: dict[str, str] = {}
    inserted = 0
    for row in payload["contents"]:
        content_id = str(
            uuid.uuid5(uuid.NAMESPACE_URL, f"arts-seed-{team_id}-{row['id']}")
        )
        content_ids[row["id"]] = content_id
        existing = db.get(Content, content_id)
        if existing:
            if existing.team_id != team_id:
                raise ValueError("示例内容 ID 已被其他团队使用")
            continue
        owner = user_map[row["owner_email"]]
        likes = int(row.get("likes", 0))
        comments = int(row.get("comments", 0))
        saves = int(row.get("saves", 0))
        shares = int(row.get("shares", 0))
        item = Content(
            id=content_id,
            team_id=team_id,
            title=row["title"],
            category=row["category"],
            platform=row["platform"],
            published_at=datetime.fromisoformat(row["published_at"]).replace(
                tzinfo=None
            ),
            duration=int(row.get("duration", 0)),
            views=int(row.get("views", 0)),
            likes=likes,
            comments=comments,
            saves=saves,
            shares=shares,
            completion=float(row.get("completion", 0)),
            total_interactions=likes + comments + saves + shares,
            tags_json=json.dumps(row.get("tags", []), ensure_ascii=False),
            source=SEED_SOURCE,
            lifecycle=row["lifecycle"],
            materials=row.get("materials", ""),
            cover_url=row.get("cover_url", ""),
            owner=owner.name,
            notes=row.get("notes", ""),
            owner_user_id=owner.id,
            performance_id=performance_ids[row["performance_id"]],
            workflow_state=row["workflow_state"],
        )
        db.add(item)
        db.flush()
        sync_tag_links(db, item, row.get("tags", []), owner.id)
        inserted += 1

    db.flush()
    for row in payload.get("schedules", []):
        content_id = content_ids[row["content_id"]]
        if db.scalar(
            select(Schedule.id).where(
                Schedule.team_id == team_id,
                Schedule.content_id == content_id,
                Schedule.status == "待发布",
            )
        ):
            continue
        owner = user_map[row["owner_email"]]
        content = db.get(Content, content_id)
        if content is None:
            raise ValueError(f"示例排期关联的内容不存在: {content_id}")
        db.add(
            Schedule(
                team_id=team_id,
                content_id=content_id,
                planned_date=date.fromisoformat(row["planned_date"]),
                planned_time=row["planned_time"],
                title=content.title,
                category=content.category,
                platform=content.platform,
                owner=owner.name,
                source=row["source"],
                created_by_user_id=owner.id,
            )
        )
    db.flush()
    db.commit()
    return inserted
