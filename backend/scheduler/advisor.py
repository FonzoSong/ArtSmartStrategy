# SPDX-License-Identifier: Apache-2.0
"""Pure scheduling recommendations constrained by an immutable ruleset."""

from __future__ import annotations

from collections import defaultdict
from datetime import date, datetime, time, timedelta
from typing import Any

from backend.settings import PLATFORMS

WEEKDAYS = ("周一", "周二", "周三", "周四", "周五", "周六", "周日")


def build_schedule_recommendations(
    published: list[Any],
    unpublished: list[Any],
    rule_set: dict[str, Any],
    now: datetime,
    min_sample: int = 5,
    existing_schedule: list[Any] | None = None,
) -> list[dict[str, Any]]:
    """Create deterministic suggestions for unpublished assets.

    Args:
        published: Published records used only as performance history.
        unpublished: Draft records that can receive schedule suggestions.
        rule_set: Validated read-only ruleset.
        now: Current local datetime, injected to make the planner deterministic.
        min_sample: Sample size needed for medium confidence.
        existing_schedule: Existing schedules used for cadence limits.

    Returns:
        Recommendations with target IDs, dates, ruleset reasons, and confidence.
    """
    policy = rule_set["schedule_policy"]
    category_rules = policy["category_duration"]
    horizon = policy["planning_horizon_days"]
    eligible_days = {WEEKDAYS.index(day) for day in policy["weekdays"]}
    slots = [time.fromisoformat(value) for value in policy["time_windows"]]
    history: dict[tuple[str, str], list[Any]] = defaultdict(list)
    for record in published:
        history[(record.category, record.platform)].append(record)

    allowed_platforms = policy["platforms"]
    platform_per_week: dict[tuple[str, int, int], int] = defaultdict(int)
    daily_count: dict[date, int] = defaultdict(int)
    scheduled_content_ids: set[str] = set()
    for schedule in existing_schedule or []:
        scheduled_date = _schedule_value(schedule, "planned_date")
        scheduled_platform = _schedule_value(schedule, "platform")
        scheduled_content_id = _schedule_value(schedule, "content_id")
        if isinstance(scheduled_date, str):
            scheduled_date = date.fromisoformat(scheduled_date)
        if scheduled_content_id:
            scheduled_content_ids.add(str(scheduled_content_id))
        if isinstance(scheduled_date, date):
            key = scheduled_date.isocalendar()
            platform_per_week[(scheduled_platform, key.year, key.week)] += 1
            daily_count[scheduled_date] += 1
    results: list[dict[str, Any]] = []
    for item in unpublished:
        if str(item.id) in scheduled_content_ids:
            continue
        category_rule = category_rules.get(item.category)
        if category_rule is None:
            continue
        if not (
            category_rule["min_minutes"]
            <= item.duration
            <= category_rule["max_minutes"]
        ):
            continue

        platform = (
            item.platform
            if item.platform in allowed_platforms
            else _best_historical_platform(
                item.category, history, allowed_platforms
            )
        )
        planned = _next_slot(
            now,
            horizon,
            eligible_days,
            slots,
            platform,
            policy["max_per_platform_per_week"],
            policy["max_per_day"],
            platform_per_week,
            daily_count,
        )
        if planned is None:
            continue
        planned_date, planned_time = planned
        iso_week = planned_date.isocalendar()
        week_key = (platform, iso_week.year, iso_week.week)
        platform_per_week[week_key] += 1
        daily_count[planned_date] += 1

        samples = history[(item.category, platform)]
        views = _mean(samples, "views")
        likes = _mean(samples, "likes")
        saves = _mean(samples, "saves")
        comments = _mean(samples, "comments")
        completion = _mean(samples, "completion")
        confidence = (
            "high"
            if len(samples) >= min_sample * 2
            else "medium"
            if len(samples) >= min_sample
            else "low"
        )
        reason = (
            f"规则集《{rule_set['name']}》schedule_policy.category_duration."
            f"{item.category}：{category_rule['reason']}；"
            f"schedule_policy.weekdays 与 time_windows 允许该时段，"
            f"且未超过每日 {policy['max_per_day']} 条、"
            f"单平台每周 {policy['max_per_platform_per_week']} 条限制。"
        )
        if samples:
            reason += (
                f" 历史依据来自 {len(samples)} 条已发布的同品类/平台内容："
                f"平均播放 {views:.0f}、点赞 {likes:.0f}、收藏 {saves:.0f}、"
                f"评论 {comments:.0f}、完播率 {completion:.1f}%。"
            )
        else:
            reason += " 当前没有同品类/平台已发布样本，置信度为低。"

        results.append(
            {
                "target_content_id": item.id,
                "planned_date": planned_date.isoformat(),
                "content_type": item.category,
                "platform": platform,
                "owner": getattr(item, "owner", "") or "",
                "day_of_week": WEEKDAYS[planned_date.weekday()],
                "time_slot": planned_time.strftime("%H:%M"),
                "title": item.title,
                "reason": reason,
                "reference_sample_count": len(samples),
                "confidence": confidence,
                "priority": max(1, len(samples)),
                "prediction_views_low": round(views * 0.7) if samples else None,
                "prediction_views_high": round(views * 1.3)
                if samples
                else None,
            }
        )
    return results


def _mean(records: list[Any], field: str) -> float:
    """Return a field mean, or zero for an empty group."""
    if not records:
        return 0.0
    return sum(float(getattr(item, field, 0) or 0) for item in records) / len(
        records
    )


def _best_historical_platform(
    category: str,
    history: dict[tuple[str, str], list[Any]],
    allowed_platforms: list[str],
) -> str:
    """Choose the historical platform with the strongest mean engagement."""
    candidates = [
        platform
        for platform in allowed_platforms
        if (category, platform) in history
    ]
    if not candidates:
        return allowed_platforms[0] if allowed_platforms else PLATFORMS[0]
    return max(
        candidates,
        key=lambda platform: (
            _mean(history[(category, platform)], "views")
            + 10 * _mean(history[(category, platform)], "likes")
            + 15 * _mean(history[(category, platform)], "saves")
            + 10 * _mean(history[(category, platform)], "comments")
        ),
    )


def _next_slot(
    now: datetime,
    horizon: int,
    eligible_days: set[int],
    slots: list[time],
    platform: str,
    platform_limit: int,
    daily_limit: int,
    platform_per_week: dict[tuple[str, int, int], int],
    daily_count: dict[date, int],
) -> tuple[date, time] | None:
    """Find the first date/time satisfying every schedule constraint."""
    for offset in range(horizon):
        candidate_date = now.date() + timedelta(days=offset)
        iso_week = candidate_date.isocalendar()
        week_key = (platform, iso_week.year, iso_week.week)
        if candidate_date.weekday() not in eligible_days:
            continue
        if platform_per_week[week_key] >= platform_limit:
            continue
        if daily_count[candidate_date] >= daily_limit:
            continue
        for slot in slots:
            if offset == 0 and slot <= now.time().replace(
                second=0, microsecond=0
            ):
                continue
            return candidate_date, slot
    return None


def _schedule_value(schedule: Any, field: str) -> Any:
    """Read a schedule field from an ORM object or mapping."""
    if isinstance(schedule, dict):
        return schedule.get(field)
    return getattr(schedule, field, None)
