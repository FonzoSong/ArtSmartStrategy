# SPDX-License-Identifier: Apache-2.0
"""Fallback handling tests for AI schedule analysis."""

from __future__ import annotations

import asyncio
import secrets
from datetime import datetime
from types import SimpleNamespace
from typing import Any

import pytest
from backend import main


class FakeSession:
    """Capture persistence calls without connecting to a database."""

    def __init__(self) -> None:
        self.rows: list[Any] = []

    def scalars(self, statement: Any) -> SimpleNamespace:
        """Return an empty scalar collection for the schedule query."""
        return SimpleNamespace(all=lambda: [])

    def add(self, row: Any) -> None:
        """Capture a row added by the route handler."""
        self.rows.append(row)

    def flush(self) -> None:
        """Apply the ORM-generated timestamp expected before response output."""
        for row in self.rows:
            if getattr(row, "generated_at", None) is None and hasattr(
                row, "generated_at"
            ):
                row.generated_at = datetime(2026, 10, 12)

    def commit(self) -> None:
        """Accept the route's transaction boundary without persistence."""
        self.flush()
        return None


class TimeoutClient:
    """Simulate a cancelled external request without network access."""

    async def __aenter__(self) -> TimeoutClient:
        return self

    async def __aexit__(self, *args: Any) -> None:
        return None

    async def post(self, *args: Any, **kwargs: Any) -> Any:
        """Raise a timeout immediately to avoid a network wait in tests."""
        raise asyncio.TimeoutError


@pytest.mark.anyio
async def test_ai_timeout_returns_rule_result_and_reason(monkeypatch) -> None:
    """A 60-second timeout degrades exactly once to the local engine."""
    config = SimpleNamespace(
        enabled=True,
        base_url="https://model.example.invalid/v1",
        api_key=secrets.token_urlsafe(24),
        model="qwen3.8-max",
        request_mode="base",
        auth_type="bearer",
        temperature=0.2,
        min_sample=5,
    )
    monkeypatch.setattr(main, "published_contents", lambda *_: [])
    monkeypatch.setattr(
        main,
        "unpublished_contents",
        lambda *_: [
            SimpleNamespace(
                id="draft-1",
                category="艺术科普",
                platform="内容社区C",
                duration=3,
                title="题目",
                owner="",
            )
        ],
    )
    monkeypatch.setattr(main, "get_config", lambda *_: config)
    monkeypatch.setattr(
        main,
        "build_schedule_recommendations",
        lambda *_args, **_kwargs: [
            {
                "target_content_id": "draft-1",
                "planned_date": "2026-10-13",
                "day_of_week": "周二",
                "time_slot": "12:30",
                "title": "题目",
                "content_type": "艺术科普",
                "platform": "内容社区C",
                "reason": "规则依据",
                "reference_sample_count": 0,
                "confidence": "low",
                "priority": 1,
            }
        ],
    )
    monkeypatch.setattr(main.httpx, "AsyncClient", lambda **_: TimeoutClient())
    monkeypatch.setattr(main, "audit", lambda *_args, **_kwargs: None)
    response = await main.ai_recommend(
        db=FakeSession(), tid=101, _role="owner", force_rule=False
    )

    assert response["source"] == "rules-engine"
    assert response["analysis_status"] == "fallback_timeout"
    assert "60 秒" in response["fallback_reason"]
    assert response["recommendations"][0]["target_content_id"] == "draft-1"


@pytest.mark.anyio
async def test_unconfigured_ai_uses_rules_for_seconds_based_duration(
    monkeypatch,
) -> None:
    """A normal two-minute draft remains eligible for local rule scheduling."""
    config = SimpleNamespace(
        enabled=True,
        base_url="",
        api_key="",
        model="qwen3.8-max",
        min_sample=5,
    )
    monkeypatch.setattr(main, "published_contents", lambda *_: [])
    monkeypatch.setattr(
        main,
        "unpublished_contents",
        lambda *_: [
            SimpleNamespace(
                id="draft-seconds",
                category="艺术科普",
                platform="内容社区C",
                duration=120,
                title="两分钟艺术科普",
                owner="",
            )
        ],
    )
    monkeypatch.setattr(main, "get_config", lambda *_: config)
    monkeypatch.setattr(main, "audit", lambda *_args, **_kwargs: None)

    response = await main.ai_recommend(
        db=FakeSession(), tid=101, _role="owner", force_rule=False
    )

    assert response["source"] == "rules-engine"
    assert response["analysis_status"] == "fallback_unconfigured"
    assert len(response["recommendations"]) == 1
    assert (
        response["recommendations"][0]["target_content_id"] == "draft-seconds"
    )
