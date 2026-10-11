# SPDX-License-Identifier: Apache-2.0
"""Exercise content creation through recommendation adoption end to end."""

from __future__ import annotations

from datetime import datetime, timedelta

from backend.auth import issue_token
from backend.database import Base, get_db
from backend.main import app
from backend.models import Content, Schedule, Team, User
from fastapi.testclient import TestClient
from sqlalchemy import create_engine, select
from sqlalchemy.orm import Session, sessionmaker
from sqlalchemy.pool import StaticPool


def test_create_recommend_and_adopt_schedule(monkeypatch) -> None:
    """An owner can create a draft, recommend it, and adopt its schedule."""
    engine = create_engine(
        "sqlite+pysqlite:///:memory:",
        connect_args={"check_same_thread": False},
        poolclass=StaticPool,
    )
    Base.metadata.create_all(engine)
    session_factory = sessionmaker(
        bind=engine, autoflush=False, expire_on_commit=False
    )
    setup_session = session_factory()
    team = Team(name="排期集成测试团队")
    owner = User(
        team_id=1,
        email="owner@schedule-test.invalid",
        name="测试负责人",
        role_name="owner",
        password_hash="not-used",
    )
    setup_session.add(team)
    setup_session.flush()
    owner.team_id = team.id
    setup_session.add(owner)
    setup_session.flush()
    token = issue_token(setup_session, owner)
    setup_session.add(
        Content(
            team_id=team.id,
            title="已发布艺术科普样本",
            category="艺术科普",
            platform="内容社区C",
            published_at=datetime.now() - timedelta(days=10),
            duration=90,
            views=12000,
            likes=800,
            comments=90,
            saves=260,
            shares=45,
            completion=72,
            total_interactions=1195,
            lifecycle="已发布",
            workflow_state="已发布",
        )
    )
    setup_session.commit()
    setup_session.close()

    def override_get_db():
        with session_factory() as session:
            yield session

    app.dependency_overrides[get_db] = override_get_db
    client = TestClient(app)
    headers = {
        "Authorization": f"Bearer {token}",
        "X-Team-Id": str(team.id),
    }
    try:
        created = client.post(
            "/api/contents",
            headers=headers,
            json={
                "title": "两分钟艺术科普待发布内容",
                "category": "艺术科普",
                "platform": "内容社区C",
                "published_at": (
                    datetime.now() + timedelta(days=2)
                ).isoformat(),
                "duration": 120,
                "lifecycle": "未发布",
                "workflow_state": "待制作",
            },
        )
        assert created.status_code == 200, created.text
        draft_id = created.json()["item"]["id"]

        generated = client.post(
            "/api/ai/recommend?force_rule=true", headers=headers
        )
        assert generated.status_code == 200, generated.text
        result = generated.json()
        assert result["source"] == "rules-engine"
        assert result["recommendations"][0]["target_content_id"] == draft_id
        assert result["recommendations"][0]["reference_sample_count"] == 1

        adopted = client.post(
            "/api/schedule/adopt",
            headers=headers,
            json={
                "recommendation_id": result["id"],
                "items": result["recommendations"],
            },
        )
        assert adopted.status_code == 200, adopted.text
        assert adopted.json()["created"] == 1

        with Session(engine) as verify_session:
            content = verify_session.get(Content, draft_id)
            schedules = verify_session.scalars(
                select(Schedule).where(
                    Schedule.content_id == draft_id,
                    Schedule.team_id == team.id,
                )
            ).all()
        assert content is not None
        assert content.lifecycle == "未发布"
        assert content.workflow_state == "已排期"
        assert len(schedules) == 1
    finally:
        app.dependency_overrides.pop(get_db, None)
        client.close()
        engine.dispose()
