# SPDX-License-Identifier: Apache-2.0
"""Validation helpers for AI-assisted schedule recommendations."""

from __future__ import annotations

import json
from datetime import date, time
from typing import Any


def build_schedule_prompt(
    candidates: list[dict[str, Any]], ruleset: dict[str, Any]
) -> str:
    """Build a constrained prompt using only matching published samples.

    Args:
        candidates: Each candidate includes one draft and same-category,
            same-platform published samples from the authenticated team.
        ruleset: Validated immutable project schedule rules.

    Returns:
        A JSON-only instruction string.
    """
    policy = ruleset["schedule_policy"]
    return (
        "你是排期分析助手。仅使用提供的团队已发布样本和规则，不能编造数据。"
        "请逐条为每个 draft_id 给出 planned_date(YYYY-MM-DD)、time_slot(HH:MM)、"
        'reason。只返回 JSON：{"recommendations":[...]}。所有 draft_id 必须且仅'
        "出现一次。日期不得超出 horizon_days；日期星期必须在 weekdays 中；时间必须"
        "属于 time_windows。样本为空时明确写样本不足，并降低可信度，不得给出效果结论。\n"
        f"约束：{json.dumps({'weekdays': policy['weekdays'], 'time_windows': policy['time_windows'], 'horizon_days': policy['planning_horizon_days'], 'max_per_day': policy['max_per_day'], 'max_per_platform_per_week': policy['max_per_platform_per_week']}, ensure_ascii=False)}\n"
        f"候选与对应历史样本：{json.dumps(candidates, ensure_ascii=False)}"
    )


def apply_ai_schedule(
    text: str,
    baseline: list[dict[str, Any]],
    ruleset: dict[str, Any],
    today: date,
    existing_schedule: list[Any] | None = None,
) -> list[dict[str, Any]]:
    """Validate AI dates and merge them onto rule-backed candidates.

    Args:
        text: Raw JSON response from an OpenAI-compatible model.
        baseline: Rule-engine recommendations, one per eligible draft.
        ruleset: Validated immutable project schedule rules.
        today: Reference date used for deterministic horizon checks.
        existing_schedule: Existing team schedules counted against cadence caps.

    Returns:
        Baseline records enhanced with validated AI date/time/reason fields.

    Raises:
        ValueError: If output is malformed, incomplete, or violates rules.
    """
    start, end = text.find("{"), text.rfind("}")
    if start < 0 or end <= start:
        raise ValueError("模型响应不是有效 JSON")
    payload = json.loads(text[start : end + 1])
    items = (
        payload.get("recommendations") if isinstance(payload, dict) else None
    )
    if not isinstance(items, list):
        raise ValueError("模型响应缺少 recommendations 列表")
    expected = {str(item["target_content_id"]) for item in baseline}
    normalized: dict[str, dict[str, str]] = {}
    policy = ruleset["schedule_policy"]
    allowed_weekdays = {
        "周一": 0,
        "周二": 1,
        "周三": 2,
        "周四": 3,
        "周五": 4,
        "周六": 5,
        "周日": 6,
    }
    allowed_slots = {
        time.fromisoformat(item).strftime("%H:%M")
        for item in policy["time_windows"]
    }
    horizon_end = today.fromordinal(
        today.toordinal() + policy["planning_horizon_days"] - 1
    )
    for item in items:
        if not isinstance(item, dict):
            raise ValueError("模型建议项格式无效")
        content_id = str(item.get("draft_id", ""))
        if content_id not in expected or content_id in normalized:
            raise ValueError("模型建议包含未知或重复的未发布内容")
        planned = date.fromisoformat(str(item.get("planned_date", "")))
        slot = time.fromisoformat(str(item.get("time_slot", ""))).strftime(
            "%H:%M"
        )
        reason = str(item.get("reason", "")).strip()
        if planned < today or planned > horizon_end:
            raise ValueError("模型建议日期超出规则集排期范围")
        if planned.weekday() not in set(
            allowed_weekdays[day] for day in policy["weekdays"]
        ):
            raise ValueError("模型建议日期不符合规则集允许星期")
        if slot not in allowed_slots:
            raise ValueError("模型建议时间不符合规则集时间窗")
        if not reason:
            raise ValueError("模型建议缺少依据说明")
        normalized[content_id] = {
            "planned_date": planned.isoformat(),
            "time_slot": slot,
            "reason": reason,
        }
    if set(normalized) != expected:
        raise ValueError("模型没有为所有未发布内容提供建议")
    daily: dict[date, int] = {}
    weekly: dict[tuple[str, int, int], int] = {}
    for scheduled in existing_schedule or []:
        scheduled_date = (
            scheduled.get("planned_date")
            if isinstance(scheduled, dict)
            else getattr(scheduled, "planned_date", None)
        )
        platform = (
            scheduled.get("platform", "")
            if isinstance(scheduled, dict)
            else getattr(scheduled, "platform", "")
        )
        if isinstance(scheduled_date, str):
            scheduled_date = date.fromisoformat(scheduled_date)
        if isinstance(scheduled_date, date):
            daily[scheduled_date] = daily.get(scheduled_date, 0) + 1
            iso = scheduled_date.isocalendar()
            key = (platform, iso.year, iso.week)
            weekly[key] = weekly.get(key, 0) + 1
    baseline_by_id = {str(item["target_content_id"]): item for item in baseline}
    for content_id, recommendation in normalized.items():
        planned = date.fromisoformat(recommendation["planned_date"])
        platform = baseline_by_id[content_id]["platform"]
        daily[planned] = daily.get(planned, 0) + 1
        iso = planned.isocalendar()
        key = (platform, iso.year, iso.week)
        weekly[key] = weekly.get(key, 0) + 1
        if daily[planned] > policy["max_per_day"]:
            raise ValueError("模型建议超过规则集每日频率上限")
        if weekly[key] > policy["max_per_platform_per_week"]:
            raise ValueError("模型建议超过规则集单平台每周频率上限")
    result = []
    for original in baseline:
        item = dict(original)
        ai = normalized[str(item["target_content_id"])]
        planned = date.fromisoformat(ai["planned_date"])
        item.update(ai)
        item["day_of_week"] = tuple(allowed_weekdays)[planned.weekday()]
        item["reason"] = (
            f"AI建议：{ai['reason']} 样本量 {item['reference_sample_count']}；"
            + (
                "样本不足，结论可信度有限。"
                if item["confidence"] == "low"
                else ""
            )
        )
        item["suggestion_source"] = "AI"
        item["analysis_status"] = "completed"
        item["limitations"] = "仅分析同团队同类型、同平台已发布内容。"
        result.append(item)
    return result
