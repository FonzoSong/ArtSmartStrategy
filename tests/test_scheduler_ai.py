# SPDX-License-Identifier: Apache-2.0
"""Deterministic validation tests for AI schedule output."""

from __future__ import annotations

import json
from datetime import date

import pytest
from backend.scheduler.ai import apply_ai_schedule, build_schedule_prompt
from backend.scheduler.rules_loader import load_rule_set


def baseline() -> list[dict[str, object]]:
    """Return one fixed rule-derived candidate for the test cases."""
    return [
        {
            "target_content_id": "draft-a",
            "planned_date": "2026-10-13",
            "time_slot": "12:30",
            "day_of_week": "周二",
            "platform": "内容社区C",
            "content_type": "艺术科普",
            "title": "内容 A",
            "reason": "rules",
            "reference_sample_count": 1,
            "confidence": "low",
        }
    ]


def test_ai_output_is_merged_with_sample_limitations() -> None:
    """AI output keeps target identity and rule-derived confidence metadata."""
    rules = load_rule_set()
    result = apply_ai_schedule(
        json.dumps(
            {
                "recommendations": [
                    {
                        "draft_id": "draft-a",
                        "planned_date": "2026-10-13",
                        "time_slot": "12:30",
                        "reason": "匹配历史表现",
                    }
                ]
            }
        ),
        baseline(),
        rules,
        date(2026, 10, 12),
    )
    assert result[0]["target_content_id"] == "draft-a"
    assert result[0]["suggestion_source"] == "AI"
    assert "可信度有限" in result[0]["reason"]


def test_ai_response_must_cover_only_valid_drafts_and_rule_dates() -> None:
    """Unknown IDs and out-of-policy dates fall back instead of appearing valid."""
    rules = load_rule_set()
    with pytest.raises(ValueError, match="未知或重复"):
        apply_ai_schedule(
            json.dumps(
                {
                    "recommendations": [
                        {
                            "draft_id": "other",
                            "planned_date": "2026-10-13",
                            "time_slot": "12:30",
                            "reason": "x",
                        }
                    ]
                }
            ),
            baseline(),
            rules,
            date(2026, 10, 12),
        )
    with pytest.raises(ValueError, match="时间窗"):
        apply_ai_schedule(
            json.dumps(
                {
                    "recommendations": [
                        {
                            "draft_id": "draft-a",
                            "planned_date": "2026-10-13",
                            "time_slot": "03:00",
                            "reason": "x",
                        }
                    ]
                }
            ),
            baseline(),
            rules,
            date(2026, 10, 12),
        )


def test_ai_prompt_only_embeds_passed_scoped_candidates() -> None:
    """Prompt has no hidden dataset expansion beyond selected candidate samples."""
    prompt = build_schedule_prompt(
        [
            {
                "draft_id": "d1",
                "published_sample_count": 1,
                "published_samples": [{"views": 99}],
            }
        ],
        load_rule_set(),
    )
    assert '"draft_id": "d1"' in prompt
    assert '"views": 99' in prompt
