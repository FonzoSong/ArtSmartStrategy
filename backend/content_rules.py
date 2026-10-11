# SPDX-License-Identifier: Apache-2.0
"""Validation helpers for team-managed content rules."""

from __future__ import annotations

import json
import re
from collections.abc import Mapping
from typing import Any

from sqlalchemy import select
from sqlalchemy.orm import Session

from backend.models import ContentRule

RULE_TYPES = {
    "required",
    "format",
    "sensitive_words",
    "length",
    "tag_constraint",
}


def rule_payload(rule: ContentRule) -> dict[str, Any]:
    """Serialize one managed rule without leaking unrelated team data."""
    try:
        config = json.loads(rule.rule_content or "{}")
    except json.JSONDecodeError:
        config = {}
    return {
        "id": rule.id,
        "name": rule.name,
        "team_id": rule.team_id,
        "content_type": rule.content_type,
        "workflow_state": rule.workflow_state,
        "rule_type": rule.rule_type,
        "rule_content": config,
        "priority": rule.priority,
        "enabled": rule.enabled,
        "notes": rule.notes,
        "updated_at": rule.updated_at.isoformat(),
        "updated_by_user_id": rule.updated_by_user_id,
    }


def _config(rule: ContentRule) -> dict[str, Any]:
    value = json.loads(rule.rule_content or "{}")
    if not isinstance(value, dict):
        raise ValueError("规则内容必须是 JSON 对象")
    return value


def evaluate_rule(rule: ContentRule, text: str, tags: list[str]) -> str | None:
    """Return a human-readable rule failure, or ``None`` when it passes."""
    config = _config(rule)
    kind = rule.rule_type
    if kind == "required":
        if not text.strip():
            return "必填内容不能为空"
    elif kind == "format":
        pattern = str(config.get("pattern", ""))
        if not pattern:
            raise ValueError("格式规则缺少 pattern")
        if not re.search(pattern, text):
            return "内容格式不符合规则"
    elif kind == "sensitive_words":
        words = config.get("words", [])
        if not isinstance(words, list):
            raise ValueError("敏感词规则 words 必须是数组")
        hits = [str(word) for word in words if str(word) and str(word) in text]
        if hits:
            return "包含敏感词：" + "、".join(hits[:8])
    elif kind == "length":
        minimum = int(config.get("min", 0))
        maximum = int(config.get("max", 300))
        if minimum < 0 or maximum < minimum:
            raise ValueError("长度规则范围非法")
        if not minimum <= len(text) <= maximum:
            return f"长度需在 {minimum} 至 {maximum} 个字符之间"
    elif kind == "tag_constraint":
        required = config.get("required", [])
        allowed = config.get("allowed")
        max_count = config.get("max_count")
        if any(value not in tags for value in required):
            return "缺少必需标签：" + "、".join(
                value for value in required if value not in tags
            )
        if allowed is not None and any(value not in allowed for value in tags):
            return "包含不允许的标签"
        if max_count is not None and len(tags) > int(max_count):
            return f"标签数量不能超过 {max_count} 个"
    return None


def applicable_rules(
    db: Session, team_id: int, content_type: str = "", workflow_state: str = ""
) -> list[ContentRule]:
    """Return enabled rules matching the team and optional scope."""
    rules = db.scalars(
        select(ContentRule)
        .where(ContentRule.team_id == team_id, ContentRule.enabled.is_(True))
        .order_by(ContentRule.priority.asc(), ContentRule.id.asc())
    ).all()
    return [
        rule
        for rule in rules
        if (not rule.content_type or rule.content_type == content_type)
        and (not rule.workflow_state or rule.workflow_state == workflow_state)
    ]


def validate_content(
    db: Session,
    team_id: int,
    *,
    text: str,
    tags: list[str],
    content_type: str,
    workflow_state: str,
) -> list[dict[str, str]]:
    """Evaluate applicable rules and return all failing rule diagnostics."""
    failures = []
    for rule in applicable_rules(db, team_id, content_type, workflow_state):
        reason = evaluate_rule(rule, text, tags)
        if reason:
            failures.append(
                {"rule_id": str(rule.id), "rule": rule.name, "message": reason}
            )
    return failures


def rule_conflict(
    db: Session,
    team_id: int,
    priority: int,
    rule_type: str,
    content_type: str,
    workflow_state: str,
    exclude_id: int | None = None,
) -> bool:
    """Return whether an active rule shares the same scope and priority."""
    del rule_type
    query = select(ContentRule.id).where(
        ContentRule.team_id == team_id,
        ContentRule.priority == priority,
        ContentRule.content_type == content_type,
        ContentRule.workflow_state == workflow_state,
        ContentRule.enabled.is_(True),
    )
    if exclude_id is not None:
        query = query.where(ContentRule.id != exclude_id)
    return db.scalar(query) is not None


def validate_rule_definition(data: Mapping[str, Any]) -> None:
    """Validate type and rule-specific configuration before persistence."""
    kind = data.get("rule_type")
    if kind not in RULE_TYPES:
        raise ValueError("不支持的规则类型")
    config = data.get("rule_content", {})
    if not isinstance(config, dict):
        raise ValueError("规则内容必须是 JSON 对象")
    try:
        if kind == "format":
            re.compile(str(config.get("pattern", "")))
        elif kind == "length":
            minimum = int(config.get("min", 0))
            maximum = int(config.get("max", 300))
            if minimum < 0 or maximum < minimum:
                raise ValueError("长度范围无效")
        elif kind == "sensitive_words" and not isinstance(
            config.get("words", []), list
        ):
            raise ValueError("words 必须是数组")
    except re.error as exc:
        raise ValueError(f"正则表达式无效：{exc}") from exc
