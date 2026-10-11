# SPDX-License-Identifier: Apache-2.0
"""SQLAlchemy models for application data."""

from __future__ import annotations

import uuid
from datetime import date, datetime, timezone

from sqlalchemy import (
    Boolean,
    CheckConstraint,
    DateTime,
    Float,
    ForeignKey,
    Integer,
    String,
    Text,
)
from sqlalchemy.orm import Mapped, mapped_column

from backend.database import Base
from backend.settings import MIN_SAMPLE_DEFAULT


def _utc_now() -> datetime:
    """Return current UTC as a naive datetime, matching existing DB columns."""
    return datetime.now(timezone.utc).replace(tzinfo=None)


class Team(Base):
    """A workspace that owns content and team member records."""

    __tablename__ = "team"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    name: Mapped[str] = mapped_column(String(100), default="示例艺术团队")
    created_at: Mapped[datetime] = mapped_column(DateTime, default=_utc_now)


class User(Base):
    """A member account with a team-scoped role."""

    __tablename__ = "user_account"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    email: Mapped[str] = mapped_column(String(254), unique=True, index=True)
    name: Mapped[str] = mapped_column(String(100))
    role_name: Mapped[str] = mapped_column(String(20), default="editor")
    password_hash: Mapped[str] = mapped_column(String(256))
    created_at: Mapped[datetime] = mapped_column(DateTime, default=_utc_now)


class UserSession(Base):
    """A revocable login session for an account."""

    __tablename__ = "user_session"
    id: Mapped[str] = mapped_column(String(36), primary_key=True)
    user_id: Mapped[int] = mapped_column(
        ForeignKey("user_account.id"), index=True
    )
    expires_at: Mapped[datetime] = mapped_column(DateTime)
    revoked: Mapped[bool] = mapped_column(Boolean, default=False)


class Content(Base):
    """A content asset and its published performance measurements."""

    __tablename__ = "content"
    __table_args__ = (
        CheckConstraint(
            "lifecycle IN ('已发布', '未发布')",
            name="ck_content_lifecycle_two_states",
        ),
    )
    id: Mapped[str] = mapped_column(
        String(36), primary_key=True, default=lambda: str(uuid.uuid4())
    )
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    title: Mapped[str] = mapped_column(String(300))
    category: Mapped[str] = mapped_column(String(40), default="演出宣传")
    platform: Mapped[str] = mapped_column(String(100))
    published_at: Mapped[datetime] = mapped_column(DateTime, index=True)
    duration: Mapped[int] = mapped_column(Integer, default=0)
    views: Mapped[int] = mapped_column(Integer, default=0)
    likes: Mapped[int] = mapped_column(Integer, default=0)
    comments: Mapped[int] = mapped_column(Integer, default=0)
    saves: Mapped[int] = mapped_column(Integer, default=0)
    shares: Mapped[int] = mapped_column(Integer, default=0)
    completion: Mapped[float] = mapped_column(Float, default=0)
    total_interactions: Mapped[int] = mapped_column(Integer, default=0)
    tags_json: Mapped[str] = mapped_column(Text, default="[]")
    source: Mapped[str] = mapped_column(String(30), default="manual")
    lifecycle: Mapped[str] = mapped_column(String(20), default="已发布")
    materials: Mapped[str] = mapped_column(Text, default="")
    owner: Mapped[str] = mapped_column(String(100), default="")
    notes: Mapped[str] = mapped_column(Text, default="")
    profile_visits: Mapped[int] = mapped_column(Integer, default=0)
    follows: Mapped[int] = mapped_column(Integer, default=0)
    attendance: Mapped[int] = mapped_column(Integer, default=0)
    tickets: Mapped[int] = mapped_column(Integer, default=0)
    comment_text: Mapped[str] = mapped_column(Text, default="")
    adopted: Mapped[bool] = mapped_column(Boolean, default=False)
    recommendation_id: Mapped[str | None] = mapped_column(
        ForeignKey("recommendation.id"), nullable=True
    )
    created_by: Mapped[str] = mapped_column(String(100), default="团队运营者")
    created_at: Mapped[datetime] = mapped_column(DateTime, default=_utc_now)
    updated_at: Mapped[datetime] = mapped_column(
        DateTime, default=_utc_now, onupdate=_utc_now
    )


class Tag(Base):
    """A team-scoped content tag."""

    __tablename__ = "tag"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    name: Mapped[str] = mapped_column(String(50))
    color: Mapped[str] = mapped_column(String(20), default="#647383")


class ContentTag(Base):
    """Association between a content record and a tag."""

    __tablename__ = "content_tag"
    content_id: Mapped[str] = mapped_column(
        ForeignKey("content.id", ondelete="CASCADE"), primary_key=True
    )
    tag_id: Mapped[int] = mapped_column(
        ForeignKey("tag.id", ondelete="CASCADE"), primary_key=True
    )


class Recommendation(Base):
    """A generated set of content recommendations."""

    __tablename__ = "recommendation"
    id: Mapped[str] = mapped_column(
        String(36), primary_key=True, default=lambda: str(uuid.uuid4())
    )
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    generated_at: Mapped[datetime] = mapped_column(DateTime, default=_utc_now)
    payload_json: Mapped[str] = mapped_column(Text)
    source_model: Mapped[str] = mapped_column(String(100), default="rule")
    source_label: Mapped[str] = mapped_column(
        String(120), default="规则分析（未使用大模型）"
    )
    confidence: Mapped[str] = mapped_column(String(20), default="low")
    reference_count: Mapped[int] = mapped_column(Integer, default=0)
    adopted: Mapped[bool] = mapped_column(Boolean, default=False)


class Schedule(Base):
    """A planned publication and its actual performance feedback."""

    __tablename__ = "schedule"
    id: Mapped[str] = mapped_column(
        String(36), primary_key=True, default=lambda: str(uuid.uuid4())
    )
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    recommendation_id: Mapped[str | None] = mapped_column(
        ForeignKey("recommendation.id"), nullable=True
    )
    content_id: Mapped[str | None] = mapped_column(
        ForeignKey("content.id"), nullable=True
    )
    planned_date: Mapped[date] = mapped_column()
    planned_time: Mapped[str] = mapped_column(String(10), default="19:30")
    title: Mapped[str] = mapped_column(String(300))
    category: Mapped[str] = mapped_column(String(40))
    platform: Mapped[str] = mapped_column(String(100))
    owner: Mapped[str] = mapped_column(String(100), default="")
    reason: Mapped[str] = mapped_column(Text, default="")
    status: Mapped[str] = mapped_column(String(20), default="待发布")
    actual_views: Mapped[int | None] = mapped_column(Integer, nullable=True)
    actual_likes: Mapped[int | None] = mapped_column(Integer, nullable=True)
    actual_comments: Mapped[int | None] = mapped_column(Integer, nullable=True)
    actual_saves: Mapped[int | None] = mapped_column(Integer, nullable=True)
    actual_shares: Mapped[int | None] = mapped_column(Integer, nullable=True)
    actual_completion: Mapped[float | None] = mapped_column(
        Float, nullable=True
    )
    prediction_views_low: Mapped[int | None] = mapped_column(
        Integer, nullable=True
    )
    prediction_views_high: Mapped[int | None] = mapped_column(
        Integer, nullable=True
    )
    created_at: Mapped[datetime] = mapped_column(DateTime, default=_utc_now)


class AIConfig(Base):
    """Model provider settings and analysis weights for a team."""

    __tablename__ = "ai_config"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    name: Mapped[str] = mapped_column(String(100), default="默认模型")
    base_url: Mapped[str] = mapped_column(Text, default="")
    api_key: Mapped[str] = mapped_column(Text, default="")
    model: Mapped[str] = mapped_column(String(150), default="")
    temperature: Mapped[float] = mapped_column(Float, default=0.4)
    request_mode: Mapped[str] = mapped_column(String(20), default="base")
    auth_type: Mapped[str] = mapped_column(String(30), default="bearer")
    enabled: Mapped[bool] = mapped_column(Boolean, default=True)
    fallback_enabled: Mapped[bool] = mapped_column(Boolean, default=True)
    score_completion_weight: Mapped[float] = mapped_column(Float, default=0.68)
    score_interaction_weight: Mapped[float] = mapped_column(Float, default=1.15)
    min_sample: Mapped[int] = mapped_column(Integer, default=MIN_SAMPLE_DEFAULT)
    platform_weights_json: Mapped[str] = mapped_column(Text, default="{}")
    updated_at: Mapped[datetime] = mapped_column(
        DateTime, default=_utc_now, onupdate=_utc_now
    )


class AICallLog(Base):
    """Audit record for an external model request."""

    __tablename__ = "ai_call_log"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    created_at: Mapped[datetime] = mapped_column(DateTime, default=_utc_now)
    input_summary: Mapped[str] = mapped_column(Text, default="")
    output_summary: Mapped[str] = mapped_column(Text, default="")
    elapsed_ms: Mapped[int] = mapped_column(Integer, default=0)
    status: Mapped[str] = mapped_column(String(30), default="ok")


class Benchmark(Base):
    """A team-owned baseline for content performance."""

    __tablename__ = "benchmark"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    category: Mapped[str] = mapped_column(String(40))
    platform: Mapped[str] = mapped_column(String(100), default="")
    completion_median: Mapped[float] = mapped_column(Float, default=0)
    interaction_median: Mapped[float] = mapped_column(Float, default=0)
    source_type: Mapped[str] = mapped_column(String(30), default="self_history")


class AuditLog(Base):
    """A record of a user action that changed team data."""

    __tablename__ = "audit_log"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    actor: Mapped[str] = mapped_column(String(100), default="团队运营者")
    action: Mapped[str] = mapped_column(String(60))
    entity_type: Mapped[str] = mapped_column(String(50))
    entity_id: Mapped[str] = mapped_column(String(80), default="")
    created_at: Mapped[datetime] = mapped_column(DateTime, default=_utc_now)
    detail: Mapped[str] = mapped_column(Text, default="")


class Hypothesis(Base):
    """A content strategy assumption and its review result."""

    __tablename__ = "hypothesis"
    id: Mapped[str] = mapped_column(
        String(36), primary_key=True, default=lambda: str(uuid.uuid4())
    )
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    statement: Mapped[str] = mapped_column(Text)
    result: Mapped[str] = mapped_column(Text, default="")
    status: Mapped[str] = mapped_column(String(20), default="待验证")
    created_at: Mapped[datetime] = mapped_column(DateTime, default=_utc_now)


class ContentRule(Base):
    """A configured rule that transforms matching content records."""

    __tablename__ = "content_rule"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    name: Mapped[str] = mapped_column(String(120))
    priority: Mapped[int] = mapped_column(Integer, default=100)
    enabled: Mapped[bool] = mapped_column(Boolean, default=True)
    trigger: Mapped[str] = mapped_column(String(40), default="content.create")
    logic: Mapped[str] = mapped_column(String(10), default="all")
    conditions_json: Mapped[str] = mapped_column(Text, default="[]")
    scope_json: Mapped[str] = mapped_column(Text, default="{}")
    actions_json: Mapped[str] = mapped_column(Text, default="[]")
    created_at: Mapped[datetime] = mapped_column(DateTime, default=_utc_now)
    updated_at: Mapped[datetime] = mapped_column(
        DateTime, default=_utc_now, onupdate=_utc_now
    )


class RuleExecutionLog(Base):
    """The result and details of one rule evaluation."""

    __tablename__ = "rule_execution_log"
    id: Mapped[int] = mapped_column(Integer, primary_key=True)
    team_id: Mapped[int] = mapped_column(ForeignKey("team.id"), index=True)
    rule_id: Mapped[int | None] = mapped_column(
        ForeignKey("content_rule.id", ondelete="SET NULL"), nullable=True
    )
    rule_name: Mapped[str] = mapped_column(String(120), default="")
    content_id: Mapped[str] = mapped_column(String(36), default="")
    trigger: Mapped[str] = mapped_column(String(40), default="")
    result: Mapped[str] = mapped_column(String(20), default="no_match")
    detail: Mapped[str] = mapped_column(Text, default="")
    created_at: Mapped[datetime] = mapped_column(DateTime, default=_utc_now)
