# SPDX-License-Identifier: Apache-2.0
"""Pydantic request schemas and API validation rules."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field, field_validator

from backend.settings import MIN_SAMPLE_DEFAULT
from backend.status import normalize_content_status


class ContentIn(BaseModel):
    """Input fields for creating or importing a content asset."""

    title: str = Field(min_length=1, max_length=300)
    category: str = "演出宣传"
    platform: str = Field(min_length=1, max_length=100)
    published_at: datetime
    duration: int = Field(default=0, ge=0)
    views: int = Field(default=0, ge=0)
    likes: int = Field(default=0, ge=0)
    comments: int = Field(default=0, ge=0)
    saves: int = Field(default=0, ge=0)
    shares: int = Field(default=0, ge=0)
    completion: float = Field(default=0, ge=0, le=100)
    tags: list[str] = Field(default_factory=list)
    source: str = "manual"
    lifecycle: str = "已发布"
    materials: str = ""
    owner: str = ""
    notes: str = ""
    profile_visits: int = Field(default=0, ge=0)
    follows: int = Field(default=0, ge=0)
    attendance: int = Field(default=0, ge=0)
    tickets: int = Field(default=0, ge=0)
    comment_text: str = ""
    adopted: bool = False
    recommendation_id: str | None = None

    @field_validator("lifecycle", mode="before")
    @classmethod
    def normalize_content_status(cls, value: Any) -> str:
        """Normalize supported and historical status values."""
        return normalize_content_status(value)


class RuleIn(BaseModel):
    """Validated conditions, scope, and actions for a content rule."""

    name: str = Field(min_length=1, max_length=120)
    priority: int = Field(default=100, ge=0, le=10000)
    enabled: bool = True
    trigger: str = "content.create"
    logic: str = "all"
    conditions: list[dict[str, Any]] = Field(default_factory=list)
    scope: dict[str, Any] = Field(default_factory=dict)
    actions: list[dict[str, Any]] = Field(default_factory=list)


class BulkContentIn(BaseModel):
    """Requested bulk operation on a set of content records."""

    ids: list[str] = Field(min_length=1, max_length=500)
    action: str
    status: str | None = None


class AIConfigIn(BaseModel):
    """Model provider and scoring configuration submitted by a team."""

    name: str = "默认模型"
    base_url: str = ""
    api_key: str = ""
    model: str = ""
    temperature: float = Field(default=0.4, ge=0, le=2)
    request_mode: str = "base"
    auth_type: str = "bearer"
    enabled: bool = True
    fallback_enabled: bool = True
    score_completion_weight: float = Field(default=0.68, ge=0, le=1)
    score_interaction_weight: float = Field(default=1.15, ge=0, le=3)
    min_sample: int = Field(default=MIN_SAMPLE_DEFAULT, ge=2, le=1000)
    platform_weights: dict[str, dict[str, float]] = Field(default_factory=dict)


class TeamIn(BaseModel):
    """Input used to create or rename a team."""

    name: str = Field(min_length=1, max_length=100)


class AdoptIn(BaseModel):
    """Recommendation items selected for the publication schedule."""

    recommendation_id: str
    items: list[dict[str, Any]]


class PublishIn(BaseModel):
    """Actual publication metrics submitted for a scheduled item."""

    title: str
    category: str
    platform: str
    published_at: datetime
    views: int = Field(ge=0)
    likes: int = Field(ge=0)
    comments: int = Field(default=0, ge=0)
    saves: int = Field(default=0, ge=0)
    shares: int = Field(default=0, ge=0)
    completion: float = Field(ge=0, le=100)
    profile_visits: int = Field(default=0, ge=0)
    follows: int = Field(default=0, ge=0)
    attendance: int = Field(default=0, ge=0)
    tickets: int = Field(default=0, ge=0)
    comment_text: str = ""


class SampleSetting(BaseModel):
    """Minimum data sample size used for analysis."""

    min_sample: int = Field(ge=2, le=1000)


class AuthSetup(BaseModel):
    """Credentials and names used to create a team owner account."""

    email: str = Field(min_length=5, max_length=254)
    password: str = Field(min_length=10, max_length=200)
    name: str = Field(min_length=1, max_length=100)
    team_name: str = Field(min_length=1, max_length=100)


class LoginIn(BaseModel):
    """Email and password submitted for authentication."""

    email: str
    password: str


class UserInvite(BaseModel):
    """Account details and role for a new team member."""

    email: str
    password: str = Field(min_length=10, max_length=200)
    name: str
    role: str = "editor"
