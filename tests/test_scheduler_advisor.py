# SPDX-License-Identifier: Apache-2.0
"""Deterministic schedule advisor tests using fixed records and time."""

from __future__ import annotations

from datetime import datetime
from types import SimpleNamespace
from typing import Any

from backend.scheduler.advisor import build_schedule_recommendations
from backend.scheduler.rules_loader import load_rule_set


def content(**values: Any) -> SimpleNamespace:
    """Create a simple record with the attributes consumed by the planner."""
    defaults = {
        "id": "draft-1",
        "title": "待发布的艺术科普",
        "category": "艺术科普",
        "platform": "内容社区C",
        "duration": 3,
        "views": 0,
        "likes": 0,
        "saves": 0,
        "comments": 0,
        "completion": 0,
    }
    defaults.update(values)
    return SimpleNamespace(**defaults)


def test_advisor_uses_published_metrics_and_only_suggests_drafts() -> None:
    """Published metrics inform a schedule assigned to an unpublished ID."""
    published = [
        content(
            id="published-1",
            lifecycle="已发布",
            views=1200,
            likes=80,
            saves=20,
            comments=15,
            completion=66,
        )
    ]
    drafts = [content()]
    suggestions = build_schedule_recommendations(
        published,
        drafts,
        load_rule_set(),
        datetime(2026, 10, 12, 20, 0),
        min_sample=2,
    )

    assert len(suggestions) == 1
    item = suggestions[0]
    assert item["target_content_id"] == "draft-1"
    assert item["planned_date"] == "2026-10-13"
    assert item["time_slot"] == "12:30"
    assert item["reference_sample_count"] == 1
    assert item["confidence"] == "low"
    assert "schedule_policy.category_duration.艺术科普" in item["reason"]
    assert "平均播放 1200" in item["reason"]


def test_empty_history_keeps_reason_explicit_and_empty_drafts_return_empty() -> (
    None
):
    """Empty inputs remain explainable and never invent draft targets."""
    ruleset = load_rule_set()
    suggestion = build_schedule_recommendations(
        [], [content()], ruleset, datetime(2026, 10, 12, 10, 0)
    )[0]
    assert suggestion["reference_sample_count"] == 0
    assert suggestion["confidence"] == "low"
    assert "没有同品类/平台已发布样本" in suggestion["reason"]
    assert (
        build_schedule_recommendations(
            [content()], [], ruleset, datetime(2026, 10, 12, 10, 0)
        )
        == []
    )


def test_duration_constraint_filters_invalid_draft() -> None:
    """Duration limits are applied in minutes to stored seconds."""
    suggestions = build_schedule_recommendations(
        [],
        [content(duration=420)],
        load_rule_set(),
        datetime(2026, 10, 12, 10, 0),
    )
    assert suggestions == []


def test_duration_rule_accepts_boundary_in_seconds() -> None:
    """A six-minute educational video meets the configured upper bound."""
    suggestions = build_schedule_recommendations(
        [],
        [content(duration=360)],
        load_rule_set(),
        datetime(2026, 10, 12, 10, 0),
    )

    assert len(suggestions) == 1
    assert suggestions[0]["target_content_id"] == "draft-1"


def test_platform_week_and_daily_limits_are_respected() -> None:
    """Daily and weekly caps prevent overloaded schedules."""
    ruleset = load_rule_set()
    policy = ruleset["schedule_policy"]
    policy["max_per_day"] = 1
    drafts = [content(id=f"draft-{index}") for index in range(3)]

    suggestions = build_schedule_recommendations(
        [], drafts, ruleset, datetime(2026, 10, 12, 10, 0)
    )

    assert len(suggestions) == 3
    assert len({item["planned_date"] for item in suggestions}) == 3


def test_existing_schedule_prevents_duplicate_target_recommendations() -> None:
    """A scheduled unpublished asset is not suggested a second time."""
    existing = [
        {
            "content_id": "draft-1",
            "planned_date": "2026-10-13",
            "platform": "内容社区C",
        }
    ]
    suggestions = build_schedule_recommendations(
        [],
        [content()],
        load_rule_set(),
        datetime(2026, 10, 12, 10, 0),
        existing_schedule=existing,
    )

    assert suggestions == []


def test_existing_weekly_platform_frequency_is_counted() -> None:
    """Existing plans consume weekly platform capacity from the ruleset."""
    existing = [
        {
            "content_id": "other-1",
            "planned_date": "2026-10-13",
            "platform": "内容社区C",
        },
        {
            "content_id": "other-2",
            "planned_date": "2026-10-14",
            "platform": "内容社区C",
        },
    ]
    suggestions = build_schedule_recommendations(
        [],
        [content()],
        load_rule_set(),
        datetime(2026, 10, 12, 10, 0),
        existing_schedule=existing,
    )

    assert suggestions[0]["planned_date"] == "2026-10-19"
