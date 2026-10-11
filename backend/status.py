# SPDX-License-Identifier: Apache-2.0
"""Content status normalization helpers."""

from __future__ import annotations

from typing import Any

WORKFLOW_STATES = (
    "待制作",
    "已制作",
    "待排期",
    "已排期",
    "已发布",
    "未发布",
)


def normalize_content_status(value: Any) -> str:
    """Map historical content statuses to the supported status set.

    Args:
        value: A status value from an API request, import, or stored record.

    Returns:
        ``"已发布"`` or ``"未发布"``. Unknown values default to
        ``"未发布"`` to avoid treating unverified data as published.
    """
    status = str(value or "已发布").strip()
    if status in {"已发布", "已复盘", "published", "发布"}:
        return "已发布"
    if status in {
        "未发布",
        "创意",
        "待拍",
        "待剪",
        "待审",
        "draft",
        "unpublished",
    }:
        return "未发布"
    return "未发布"
