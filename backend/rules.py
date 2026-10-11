# SPDX-License-Identifier: Apache-2.0
"""Content rule validation and execution services."""

from __future__ import annotations

import json
from collections.abc import Mapping
from typing import Any

from sqlalchemy.exc import SQLAlchemyError
from sqlalchemy.orm import Session

from backend.content import sync_tag_links
from backend.models import Content, RuleExecutionLog
from backend.scheduler.rules_loader import load_rule_set_safe
from backend.status import normalize_content_status


def _rule_value(rule: Mapping[str, Any], key: str) -> Any:
    """Read a property from a schema-validated JSON rule."""
    return rule[key]


def rule_matches(rule: Mapping[str, Any], content: Content) -> bool:
    """Evaluate whether a rule's scope and conditions match content.

    Args:
        rule: Persisted rule to evaluate.
        content: Content record being checked.

    Returns:
        Whether the content is within the rule scope and satisfies its logic.
    """
    scope = _rule_value(rule, "scope")
    if scope.get("platforms") and content.platform not in scope["platforms"]:
        return False
    if scope.get("categories") and content.category not in scope["categories"]:
        return False
    if (
        scope.get("statuses")
        and normalize_content_status(content.lifecycle) not in scope["statuses"]
    ):
        return False
    conditions = _rule_value(rule, "conditions")
    if not conditions:
        return True
    values = []
    for condition in conditions:
        actual = getattr(content, condition["field"])
        expected = condition["value"]
        op_name = condition["operator"]
        if op_name == "eq":
            matched = str(actual) == str(expected)
        elif op_name == "neq":
            matched = str(actual) != str(expected)
        elif op_name == "contains":
            matched = str(expected).casefold() in str(actual).casefold()
        else:
            try:
                left, right = float(actual), float(expected)
                matched = {
                    "gt": left > right,
                    "gte": left >= right,
                    "lt": left < right,
                    "lte": left <= right,
                }[op_name]
            except (TypeError, ValueError):
                matched = False
        values.append(matched)
    return all(values) if _rule_value(rule, "logic") == "all" else any(values)


def execute_content_rules(
    db: Session,
    tid: int,
    content: Content,
    trigger: str,
) -> list[RuleExecutionLog]:
    """Apply read-only JSON rules for a trigger and log execution results.

    Args:
        db: Active database session.
        tid: Team identifier used to scope rules.
        content: Content record to evaluate and possibly update.
        trigger: Trigger name, such as ``content.create`` or ``manual``.

    Returns:
        Logs for matched, unmatched, or invalid rule executions.
    """
    ruleset = load_rule_set_safe()
    if ruleset is None:
        return []
    rules = sorted(
        (
            rule
            for rule in ruleset["rules"]
            if rule["enabled"] and rule["trigger"] == trigger
        ),
        key=lambda rule: (-rule["priority"], rule["name"]),
    )
    logs: list[RuleExecutionLog] = []
    for rule_item in rules:
        result = "no_match"
        details: list[str] = []
        try:
            if rule_matches(rule_item, content):
                result = "matched"
                for action in _rule_value(rule_item, "actions"):
                    kind, value = action["type"], action["value"]
                    if kind == "set_status":
                        content.lifecycle = normalize_content_status(value)
                    elif kind == "add_tag":
                        tags = json.loads(content.tags_json or "[]")
                        if value not in tags:
                            tags.append(str(value))
                        content.tags_json = json.dumps(tags, ensure_ascii=False)
                        sync_tag_links(db, content, tags)
                    elif kind == "set_owner":
                        content.owner = str(value)
                    elif kind == "append_note":
                        content.notes = (
                            content.notes
                            + ("\n" if content.notes else "")
                            + str(value)
                        )[:4000]
                    elif kind == "set_category":
                        content.category = str(value)
                    details.append(f"{kind}={value}")
        except (
            json.JSONDecodeError,
            KeyError,
            SQLAlchemyError,
            TypeError,
            ValueError,
        ) as exc:
            result = "error"
            details.append(f"执行异常：{str(exc)[:300]}")
        log = RuleExecutionLog(
            team_id=tid,
            rule_id=rule_item.get("id"),
            rule_name=_rule_value(rule_item, "name"),
            content_id=content.id,
            trigger=trigger,
            result=result,
            detail="；".join(details)
            or ("条件命中" if result == "matched" else "条件未命中"),
        )
        db.add(log)
        logs.append(log)
    return logs
