# SPDX-License-Identifier: Apache-2.0
"""Regression tests for managed content rules, workflow history, and tags."""

from __future__ import annotations

import json
from datetime import date, datetime, time, timedelta

import pytest
from backend import main
from backend.content import normalize_tag_name, sync_tag_links
from backend.database import Base
from backend.models import (
    Content,
    ContentRule,
    ContentTag,
    ContentWorkflowEvent,
    Schedule,
    Tag,
    Team,
)
from backend.schemas import ContentIn, ManualScheduleIn
from fastapi import HTTPException
from sqlalchemy import create_engine, select
from sqlalchemy.orm import Session


@pytest.fixture
def session() -> Session:
    """Create an isolated in-memory schema for domain API calls."""
    engine = create_engine("sqlite+pysqlite:///:memory:")
    Base.metadata.create_all(engine)
    db = Session(engine, expire_on_commit=False)
    db.add(Team(id=1, name="云栖昆曲艺术团"))
    db.commit()
    yield db
    db.close()
    engine.dispose()


def make_content(**values: object) -> Content:
    """Build a minimal draft that satisfies the content model."""
    defaults: dict[str, object] = {
        "id": "content-managed-1",
        "team_id": 1,
        "title": "秋日演出预告",
        "category": "演出宣传",
        "platform": "短视频平台A",
        "published_at": datetime(2026, 10, 12, 19, 30),
        "lifecycle": "未发布",
        "workflow_state": "待制作",
        "tags_json": "[]",
    }
    defaults.update(values)
    return Content(**defaults)


def test_tag_normalization_handles_full_width_case_and_invalid_chars() -> None:
    """Tag identity uses NFKC/case folding and disallows unsafe separators."""
    assert normalize_tag_name("  Ａrt-Tag ") == ("Art-Tag", "art-tag")
    with pytest.raises(ValueError, match="仅允许"):
        normalize_tag_name("舞台 tag")
    with pytest.raises(ValueError, match="仅允许"):
        normalize_tag_name("舞台/排练")


def test_tag_sync_and_soft_delete_keep_history(session: Session) -> None:
    """Content links deduplicate and deactivation retains associations."""
    content = make_content()
    session.add(content)
    session.flush()
    sync_tag_links(session, content, [" 舞台 ", "舞台", "", "后台"], 4)
    session.commit()
    tags = session.scalars(select(Tag).order_by(Tag.name)).all()
    assert len(tags) == 2
    assert session.scalar(
        select(ContentTag).where(ContentTag.content_id == content.id)
    )

    source = tags[0]
    result = main.delete_tag(
        source.id, session, 1, {"user_id": 4, "name": "林知夏"}, "editor"
    )
    assert result["historical_links_retained"] is True
    assert source.is_active is False
    assert (
        session.scalar(select(ContentTag).where(ContentTag.tag_id == source.id))
        is not None
    )


def test_content_save_applies_enabled_team_rule(session: Session) -> None:
    """An enabled database rule blocks invalid content at the save boundary."""
    session.add(
        ContentRule(
            team_id=1,
            name="标题长度限制",
            rule_type="length",
            rule_content=json.dumps({"min": 1, "max": 5}),
            priority=10,
            enabled=True,
        )
    )
    session.commit()
    data = ContentIn(
        title="标题长度超过五个字",
        platform="抖音",
        published_at=datetime(2026, 10, 12, 19, 30),
        lifecycle="未发布",
        workflow_state="待制作",
    )
    claims = {"user_id": 9, "name": "周清和"}
    with pytest.raises(HTTPException) as failure:
        main.create_content(data, session, 1, claims, "owner")
    assert failure.value.status_code == 422
    assert "标题长度限制" in str(failure.value.detail)

    rule = session.scalar(select(ContentRule))
    rule.enabled = False
    session.commit()
    created = main.create_content(data, session, 1, claims, "owner")
    assert created["item"]["workflow_state"] == "待制作"


def test_advance_and_manual_schedule_record_source_and_actor(
    session: Session,
) -> None:
    """Workflow progression and manual schedule are persisted and auditable."""
    content = make_content()
    session.add(content)
    session.commit()
    claims = {"user_id": 9, "name": "周清和"}

    advanced = main.advance_content_workflow(
        content.id, session, 1, claims, "editor"
    )
    assert advanced["workflow_state"] == "已制作"

    policy = main.load_rule_set_safe()["schedule_policy"]
    candidate = date.today() + timedelta(days=1)
    schedule = main.create_manual_schedule(
        ManualScheduleIn(
            content_id=content.id,
            planned_date=datetime.combine(candidate, time.min),
            planned_time=policy["time_windows"][0],
        ),
        session,
        1,
        claims,
        "editor",
    )
    assert schedule["source"] == "manual"
    row = session.scalar(select(Schedule).where(Schedule.id == schedule["id"]))
    assert row.created_by_user_id == claims["user_id"]
    event = session.scalars(
        select(ContentWorkflowEvent)
        .where(ContentWorkflowEvent.content_id == content.id)
        .order_by(ContentWorkflowEvent.created_at.desc())
    ).first()
    assert event.to_state == "已排期"
    assert event.source == "manual"
    assert event.actor_user_id == claims["user_id"]


def test_schedule_policy_enforces_daily_limit(session: Session) -> None:
    """Manual and edited schedules share the ruleset frequency limit."""
    policy = main.load_rule_set_safe()["schedule_policy"]
    candidate = date.today() + timedelta(days=1)
    for index in range(policy["max_per_day"]):
        session.add(
            Schedule(
                team_id=1,
                planned_date=candidate,
                planned_time=policy["time_windows"][0],
                title=f"演出内容 {index + 1}",
                category="演出宣传",
                platform=policy["platforms"][0],
                status="待发布",
                source="manual",
            )
        )
    session.commit()

    with pytest.raises(HTTPException) as failure:
        main.validate_schedule_policy(
            session,
            1,
            candidate,
            policy["time_windows"][0],
            policy["platforms"][0],
        )
    assert failure.value.status_code == 422
    assert "每日发布频率" in failure.value.detail


def test_schedule_policy_rejects_platform_outside_ruleset(
    session: Session,
) -> None:
    """A platform outside the configured policy cannot be scheduled."""
    policy = main.load_rule_set_safe()["schedule_policy"]
    with pytest.raises(HTTPException) as failure:
        main.validate_schedule_policy(
            session,
            1,
            date.today() + timedelta(days=1),
            policy["time_windows"][0],
            "不在规则范围的平台",
        )
    assert failure.value.status_code == 422
    assert "平台" in failure.value.detail
