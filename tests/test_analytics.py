# SPDX-License-Identifier: Apache-2.0
"""Tests for sample-aware content analysis helpers."""

from __future__ import annotations

from datetime import datetime

import pytest
from backend.analytics import (
    content_frame,
    day_label,
    group_analysis,
    interaction_rate,
    sample_aggregate,
    sample_notice,
    score_content,
    time_label,
)
from backend.models import AIConfig, Content


def make_content(
    *,
    title: str = "舞台动作拆解",
    category: str = "技艺展示",
    views: int = 100,
    likes: int = 10,
    completion: float = 60.0,
) -> Content:
    """Build a content row with stable metrics for analytics tests."""
    return Content(
        id=title,
        team_id=1,
        title=title,
        category=category,
        platform="短视频平台A",
        published_at=datetime(2026, 10, 5, 19, 30),
        views=views,
        likes=likes,
        comments=0,
        saves=0,
        shares=0,
        completion=completion,
        total_interactions=likes,
        profile_visits=0,
        follows=0,
        attendance=0,
        tickets=0,
    )


def make_config(platform_weights_json: str = "{}") -> AIConfig:
    """Build a team analysis configuration with explicit score weights."""
    return AIConfig(
        team_id=1,
        platform_weights_json=platform_weights_json,
        score_completion_weight=0.5,
        score_interaction_weight=1.0,
    )


@pytest.mark.parametrize(
    ("count", "threshold", "expected"),
    [(1, 2, "样本不足"), (2, 2, "样本较少，仅供参考"), (5, 2, "")],
)
def test_sample_notice_tracks_configured_threshold(
    count: int, threshold: int, expected: str
) -> None:
    """Sample warning text changes at the established threshold boundaries."""
    assert sample_notice(count, threshold) == expected


def test_score_content_honors_platform_weights_and_caps_at_one_hundred() -> (
    None
):
    """Platform overrides affect score while the maximum remains bounded."""
    content = make_content(views=100, likes=20, completion=50)
    base = make_config()
    override = make_config(
        '{"短视频平台A":{"completion":1.0,"interaction":2.0}}'
    )

    assert score_content(content, base) == 45.0
    assert score_content(content, override) == 90.0
    assert (
        score_content(make_content(views=1, likes=0, completion=100), override)
        == 100
    )


def test_interaction_rate_handles_zero_views() -> None:
    """A record without views has a zero interaction rate."""
    assert interaction_rate(make_content(views=0)) == 0
    assert interaction_rate(make_content(views=100, likes=10)) == 10


def test_content_frame_handles_empty_and_populated_inputs() -> None:
    """Analysis frames retain calculated values and stable empty shape."""
    assert content_frame([]).empty
    frame = content_frame([make_content()], make_config())

    assert frame.iloc[0]["weekday"] == 0
    assert frame.iloc[0]["hour"] == 19
    assert frame.iloc[0]["interaction_rate"] == 10
    assert frame.iloc[0]["score"] == 40


def test_sample_aggregate_hides_small_samples_and_returns_adequate_mean() -> (
    None
):
    """Aggregates suppress unsupported averages and expose adequate means."""
    first = make_content(views=100)
    second = make_content(title="另一个标题", views=300)

    small = sample_aggregate([first], "views", threshold=2)
    adequate = sample_aggregate([first, second], "views", threshold=2)

    assert small["value"] is None
    assert small["confidence_note"] == "样本不足"
    assert adequate["value"] == 200


@pytest.mark.parametrize(
    ("index", "expected"), [(0, "周一"), (6, "周日"), (7, "周一")]
)
def test_day_label_wraps_weekday_indices(index: int, expected: str) -> None:
    """Weekday labels follow Monday-first order and wrap after Sunday."""
    assert day_label(index) == expected


@pytest.mark.parametrize(
    ("hour", "expected"),
    [
        (4, "夜间"),
        (5, "上午"),
        (11, "午间"),
        (14, "下午"),
        (18, "晚间"),
        (22, "夜间"),
    ],
)
def test_time_label_boundaries(hour: int, expected: str) -> None:
    """Time-of-day labels honor the configured hour intervals."""
    assert time_label(hour) == expected


def test_group_analysis_empty_and_sample_qualified_groups() -> None:
    """Groups are empty-safe and expose means only after enough samples."""
    assert group_analysis([], "category", threshold=2) == []
    records = [
        make_content(title="第一个", category="技艺展示", views=100),
        make_content(title="第二个", category="技艺展示", views=300),
        make_content(title="单条样本", category="艺术科普", views=900),
    ]

    groups = group_analysis(records, "category", threshold=2)
    by_name = {item["name"]: item for item in groups}

    assert by_name["技艺展示"]["views"] == 200
    assert by_name["艺术科普"]["views"] is None
