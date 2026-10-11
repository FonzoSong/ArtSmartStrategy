# SPDX-License-Identifier: Apache-2.0
"""Content metrics and sample-aware analysis helpers."""

from __future__ import annotations

import json
from typing import Any

import pandas as pd

from backend.models import AIConfig, Content


def score_content(c: Content, config: AIConfig) -> float:
    """Calculate the configured weighted performance score for content.

    Args:
        c: Content metrics to score.
        config: Team scoring weights and optional platform overrides.

    Returns:
        A score from zero to one hundred, rounded to one decimal place.
    """
    interaction_rate = (
        (c.likes + c.comments + c.saves + c.shares) / c.views if c.views else 0
    )
    templates = json.loads(config.platform_weights_json or "{}")
    weights = templates.get(c.platform, {})
    completion_weight = float(
        weights.get("completion", config.score_completion_weight)
    )
    interaction_weight = float(
        weights.get("interaction", config.score_interaction_weight)
    )
    return round(
        min(
            100,
            c.completion * completion_weight
            + min(interaction_rate * 100, 28) * interaction_weight,
        ),
        1,
    )


def sample_notice(n: int, threshold: int) -> str:
    """Describe the reliability of an aggregate at a sample size.

    Args:
        n: Number of records included in the aggregate.
        threshold: Minimum sample size configured by the team.

    Returns:
        The existing Chinese sample warning or an empty string.
    """
    if n < threshold:
        return "样本不足"
    return "样本较少，仅供参考" if n <= threshold * 2 else ""


def interaction_rate(c: Content) -> float:
    """Calculate interactions per view as a percentage.

    Args:
        c: Content record whose interaction rate is calculated.

    Returns:
        Percentage of views that resulted in an interaction.
    """
    return (
        (c.likes + c.comments + c.saves + c.shares) / c.views * 100
        if c.views
        else 0
    )


def content_frame(
    records: list[Content], config: AIConfig | None = None
) -> pd.DataFrame:
    """Convert content records to analysis-ready tabular data.

    Args:
        records: Content rows included in the analysis.
        config: Optional team scoring configuration.

    Returns:
        A dataframe containing metrics and derived dimensions.
    """
    rows = []
    for c in records:
        rows.append(
            {
                "id": c.id,
                "title": c.title,
                "category": c.category,
                "platform": c.platform,
                "published_at": c.published_at,
                "views": c.views,
                "completion": c.completion,
                "interaction_rate": interaction_rate(c),
                "interactions": c.total_interactions,
                "weekday": c.published_at.weekday(),
                "hour": c.published_at.hour,
                "score": score_content(c, config)
                if config
                else round(
                    c.completion * 0.68 + min(interaction_rate(c), 28) * 1.15, 1
                ),
                "profile_visits": c.profile_visits,
                "follows": c.follows,
                "attendance": c.attendance,
                "tickets": c.tickets,
            }
        )
    return pd.DataFrame(rows)


def sample_aggregate(
    records: list[Content], metric: str, threshold: int
) -> dict[str, Any]:
    """Return an average only when the sample meets its threshold.

    Args:
        records: Content rows included in the aggregate.
        metric: Numeric content attribute to average.
        threshold: Minimum sample size required to expose the value.

    Returns:
        The sample count, optional average, and confidence note.
    """
    n = len(records)
    return {
        "sample_count": n,
        "value": round(
            float(pd.Series([getattr(c, metric) for c in records]).mean()), 2
        )
        if n >= threshold
        else None,
        "confidence_note": sample_notice(n, threshold),
    }


def day_label(i: int) -> str:
    """Return the localized weekday label for an integer index.

    Args:
        i: Weekday index, with Monday equal to zero.

    Returns:
        Chinese weekday label, wrapping indices to a seven-day week.
    """
    return ["周一", "周二", "周三", "周四", "周五", "周六", "周日"][i % 7]


def time_label(hour: int) -> str:
    """Map a 24-hour clock value to a localized time-of-day label.

    Args:
        hour: Hour in the 24-hour clock.

    Returns:
        One of morning, midday, afternoon, evening, or night labels.
    """
    if 5 <= hour < 11:
        return "上午"
    if 11 <= hour < 14:
        return "午间"
    if 14 <= hour < 18:
        return "下午"
    if 18 <= hour < 22:
        return "晚间"
    return "夜间"


def group_analysis(
    records: list[Content],
    key: str,
    threshold: int,
    config: AIConfig | None = None,
) -> list[dict[str, Any]]:
    """Group content metrics and suppress averages below the threshold.

    Args:
        records: Content rows to group.
        key: Dataframe column used to form groups.
        threshold: Minimum records needed to show group averages.
        config: Optional team scoring configuration.

    Returns:
        Groups ordered by sample-qualified performance score.
    """
    frame = content_frame(records, config)
    if frame.empty:
        return []
    groups = []
    for name, group in frame.groupby(key, dropna=False):
        n = int(len(group))
        adequate = n >= threshold
        groups.append(
            {
                "name": str(name),
                "sample_count": n,
                "views": round(float(group.views.mean()), 1)
                if adequate
                else None,
                "completion": round(float(group.completion.mean()), 1)
                if adequate
                else None,
                "interaction_rate": round(
                    float(group.interaction_rate.mean()), 2
                )
                if adequate
                else None,
                "score": round(float(group.score.mean()), 1)
                if adequate
                else None,
                "confidence_note": sample_notice(n, threshold),
            }
        )
    return sorted(
        groups,
        key=lambda x: (x["score"] is not None, x["score"] or 0),
        reverse=True,
    )
