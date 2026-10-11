# SPDX-License-Identifier: Apache-2.0
"""Content rule validation and execution services."""

from __future__ import annotations

import json

from fastapi import HTTPException
from sqlalchemy import select
from sqlalchemy.exc import SQLAlchemyError
from sqlalchemy.orm import Session

from backend.content import sync_tag_links
from backend.models import Content, ContentRule, RuleExecutionLog
from backend.schemas import RuleIn
from backend.settings import CATEGORIES
from backend.status import normalize_content_status

RULE_FIELDS = {
    "platform",
    "category",
    "lifecycle",
    "views",
    "likes",
    "comments",
    "completion",
    "owner",
    "source",
}
RULE_OPERATORS = {"eq", "neq", "contains", "gt", "gte", "lt", "lte"}
RULE_ACTIONS = {
    "set_status",
    "add_tag",
    "set_owner",
    "append_note",
    "set_category",
}
RULE_TRIGGERS = {"content.create", "content.update", "content.import", "manual"}


def validate_rule_payload(data: RuleIn) -> None:
    """Reject unsupported conditions, scopes, and actions.

    Args:
        data: Rule definition submitted through the API.

    Raises:
        HTTPException: If any rule component is outside the supported schema.
    """
    if data.trigger not in RULE_TRIGGERS:
        raise HTTPException(422, "触发时机不支持")
    if data.logic not in {"all", "any"}:
        raise HTTPException(422, "条件组合只能是且（all）或或（any）")
    for condition in data.conditions:
        if (
            condition.get("field") not in RULE_FIELDS
            or condition.get("operator") not in RULE_OPERATORS
            or "value" not in condition
        ):
            raise HTTPException(422, "规则条件字段、运算符或比较值无效")
    for field in ("platforms", "categories", "statuses"):
        value = data.scope.get(field, [])
        if not isinstance(value, list) or any(
            not isinstance(item, str) for item in value
        ):
            raise HTTPException(422, f"生效范围 {field} 必须为字符串数组")
    if any(
        value not in {"已发布", "未发布"}
        for value in data.scope.get("statuses", [])
    ):
        raise HTTPException(422, "生效范围状态只能是已发布或未发布")
    for action in data.actions:
        if action.get("type") not in RULE_ACTIONS or "value" not in action:
            raise HTTPException(422, "规则动作类型或动作值无效")
        if action["type"] == "set_status" and action["value"] not in {
            "已发布",
            "未发布",
        }:
            raise HTTPException(422, "规则状态动作只能设置为已发布或未发布")
        if (
            action["type"] == "set_category"
            and action["value"] not in CATEGORIES
        ):
            raise HTTPException(422, "内容类型动作值不在可选范围内")
        if (
            action["type"] in {"add_tag", "set_owner", "append_note"}
            and not str(action["value"]).strip()
        ):
            raise HTTPException(422, "文本类规则动作值不能为空")
    if not data.actions:
        raise HTTPException(422, "至少配置一个执行动作")


def rule_matches(rule: ContentRule, content: Content) -> bool:
    """Evaluate whether a rule's scope and conditions match content.

    Args:
        rule: Persisted rule to evaluate.
        content: Content record being checked.

    Returns:
        Whether the content is within the rule scope and satisfies its logic.
    """
    scope = json.loads(rule.scope_json or "{}")
    if scope.get("platforms") and content.platform not in scope["platforms"]:
        return False
    if scope.get("categories") and content.category not in scope["categories"]:
        return False
    if (
        scope.get("statuses")
        and normalize_content_status(content.lifecycle) not in scope["statuses"]
    ):
        return False
    conditions = json.loads(rule.conditions_json or "[]")
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
    return all(values) if rule.logic == "all" else any(values)


def execute_content_rules(
    db: Session, tid: int, content: Content, trigger: str
) -> list[RuleExecutionLog]:
    """Apply enabled rules for a trigger and create execution log entries.

    Args:
        db: Active database session.
        tid: Team identifier used to scope rules.
        content: Content record to evaluate and possibly update.
        trigger: Trigger name, such as ``content.create`` or ``manual``.

    Returns:
        Logs for matched, unmatched, or invalid rule executions.
    """
    rules = db.scalars(
        select(ContentRule)
        .where(
            ContentRule.team_id == tid,
            ContentRule.enabled.is_(True),
            ContentRule.trigger == trigger,
        )
        .order_by(ContentRule.priority.desc(), ContentRule.id)
    ).all()
    logs = []
    for rule_item in rules:
        result = "no_match"
        details: list[str] = []
        try:
            if rule_matches(rule_item, content):
                result = "matched"
                for action in json.loads(rule_item.actions_json or "[]"):
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
            rule_id=rule_item.id,
            rule_name=rule_item.name,
            content_id=content.id,
            trigger=trigger,
            result=result,
            detail="；".join(details)
            or ("条件命中" if result == "matched" else "条件未命中"),
        )
        db.add(log)
        logs.append(log)
    return logs
