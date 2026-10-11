# SPDX-License-Identifier: Apache-2.0
"""Unit tests for the content rule engine."""

from __future__ import annotations

import json
from datetime import datetime
from typing import Any

import pytest
from backend.content import sync_tag_links
from backend.models import (
    Content,
    ContentRule,
    ContentTag,
    RuleExecutionLog,
    Tag,
)
from backend.rules import (
    execute_content_rules,
    rule_matches,
    validate_rule_payload,
)
from backend.schemas import RuleIn
from fastapi import HTTPException


class FakeScalars:
    """A query result that returns preconfigured rule rows."""

    def __init__(self, rows: list[ContentRule]) -> None:
        self.rows = rows

    def all(self) -> list[ContentRule]:
        """Return configured rows."""
        return self.rows


class FakeQuery:
    """A minimal delete query for tag association synchronization."""

    def filter(self, *_args: Any, **_kwargs: Any) -> FakeQuery:
        """Keep supporting the query builder interface."""
        return self

    def delete(self, **_kwargs: Any) -> int:
        """Return the number of removed rows."""
        return 0


class FakeSession:
    """A small in-memory session used to exercise rule actions."""

    def __init__(self, rules: list[ContentRule] | None = None) -> None:
        self.rules = rules or []
        self.added: list[Any] = []
        self.next_tag_id = 1

    def scalars(self, *_args: Any, **_kwargs: Any) -> FakeScalars:
        """Return configured rule rows for the engine query."""
        return FakeScalars(self.rules)

    def query(self, *_args: Any, **_kwargs: Any) -> FakeQuery:
        """Return a fake query for deleting prior tag associations."""
        return FakeQuery()

    def scalar(self, *_args: Any, **_kwargs: Any) -> None:
        """Report that a requested tag does not already exist."""
        return None

    def add(self, item: Any) -> None:
        """Collect newly created rows and assign tag identifiers."""
        if isinstance(item, Tag):
            item.id = self.next_tag_id
            self.next_tag_id += 1
        self.added.append(item)

    def flush(self) -> None:
        """Match the session flush method without a database."""


def make_content(**overrides: Any) -> Content:
    """Build a content record with explicit values for rule conditions."""
    values: dict[str, Any] = {
        "id": "content-1",
        "team_id": 7,
        "title": "练习中的舞台动作",
        "category": "技艺展示",
        "platform": "短视频平台A",
        "published_at": datetime(2026, 10, 11, 19, 30),
        "views": 1200,
        "likes": 120,
        "comments": 30,
        "completion": 65.0,
        "lifecycle": "已发布",
        "owner": "小林",
        "source": "manual",
        "tags_json": "[]",
        "notes": "已有备注",
    }
    values.update(overrides)
    return Content(**values)


def make_rule(
    conditions: list[dict[str, Any]] | None = None,
    *,
    scope: dict[str, Any] | None = None,
    actions: list[dict[str, Any]] | None = None,
    logic: str = "all",
) -> ContentRule:
    """Build a rule with JSON-encoded condition, scope, and action data."""
    return ContentRule(
        id=3,
        team_id=7,
        name="单元测试规则",
        priority=100,
        enabled=True,
        trigger="manual",
        logic=logic,
        conditions_json=json.dumps(conditions or []),
        scope_json=json.dumps(scope or {}),
        actions_json=json.dumps(actions or []),
    )


def test_validate_rule_payload_accepts_supported_rule() -> None:
    """Valid conditions, scopes, triggers, and actions are accepted."""
    validate_rule_payload(
        RuleIn(
            name="高表现内容",
            trigger="manual",
            logic="all",
            conditions=[{"field": "views", "operator": "gte", "value": 100}],
            scope={"statuses": ["已发布"]},
            actions=[{"type": "add_tag", "value": "复盘"}],
        )
    )


@pytest.mark.parametrize(
    ("payload", "message"),
    [
        ({"trigger": "unknown"}, "触发时机"),
        ({"logic": "xor"}, "条件组合"),
        (
            {
                "conditions": [
                    {"field": "unknown", "operator": "eq", "value": 1}
                ]
            },
            "规则条件",
        ),
        ({"scope": {"statuses": ["待审"]}}, "生效范围状态"),
        ({"scope": {"platforms": "短视频平台A"}}, "必须为字符串数组"),
        ({"actions": [{"type": "set_status", "value": "草稿"}]}, "状态动作"),
        ({"actions": []}, "至少配置一个"),
    ],
)
def test_validate_rule_payload_rejects_invalid_rule(
    payload: dict[str, Any], message: str
) -> None:
    """Invalid rule values produce a clear HTTP validation error."""
    values: dict[str, Any] = {
        "name": "无效规则",
        "trigger": "manual",
        "logic": "all",
        "conditions": [],
        "scope": {},
        "actions": [{"type": "add_tag", "value": "有效动作"}],
    }
    values.update(payload)

    with pytest.raises(HTTPException) as error:
        validate_rule_payload(RuleIn(**values))
    assert message in error.value.detail


@pytest.mark.parametrize(
    ("operator", "expected", "matches"),
    [
        ("eq", 1200, True),
        ("neq", 100, True),
        ("contains", "视频", True),
        ("gt", 1100, True),
        ("gte", 1200, True),
        ("lt", 1300, True),
        ("lte", 1200, True),
    ],
)
def test_rule_matches_supported_operators(
    operator: str, expected: Any, matches: bool
) -> None:
    """Every supported rule operator evaluates against record data."""
    field = "platform" if operator == "contains" else "views"
    rule = make_rule(
        [{"field": field, "operator": operator, "value": expected}]
    )

    assert rule_matches(rule, make_content()) is matches


def test_rule_matches_scope_and_and_or_logic() -> None:
    """Scope is enforced and conditions use the configured boolean logic."""
    content = make_content()
    scoped = make_rule(
        [
            {"field": "views", "operator": "gte", "value": 1000},
            {"field": "completion", "operator": "gte", "value": 70},
        ],
        scope={"platforms": ["短视频平台A"], "statuses": ["已发布"]},
    )
    either = make_rule(
        [
            {"field": "views", "operator": "gte", "value": 1000},
            {"field": "completion", "operator": "gte", "value": 70},
        ],
        logic="any",
    )

    assert not rule_matches(scoped, content)
    assert not rule_matches(
        make_rule(scope={"categories": ["演出宣传"]}), content
    )
    assert rule_matches(either, content)
    assert rule_matches(make_rule(), content)


def test_rule_matches_invalid_numeric_comparison_as_false() -> None:
    """Non-numeric values do not match numeric operators."""
    rule = make_rule([{"field": "owner", "operator": "gt", "value": "负责人"}])

    assert not rule_matches(rule, make_content())


def test_rule_execution_applies_actions_and_records_match() -> None:
    """A matching rule applies all supported action types and logs success."""
    actions = [
        {"type": "set_status", "value": "未发布"},
        {"type": "add_tag", "value": "自动标记"},
        {"type": "set_owner", "value": "新负责人"},
        {"type": "append_note", "value": "请复盘开头表达。"},
        {"type": "set_category", "value": "艺术科普"},
    ]
    session = FakeSession([make_rule(actions=actions)])
    content = make_content()

    logs = execute_content_rules(session, 7, content, "manual")

    assert logs[0].result == "matched"
    assert content.lifecycle == "未发布"
    assert content.owner == "新负责人"
    assert content.category == "艺术科普"
    assert content.notes.endswith("请复盘开头表达。")
    assert "自动标记" in content.tags_json
    assert any(isinstance(row, ContentTag) for row in session.added)
    assert any(isinstance(row, RuleExecutionLog) for row in session.added)


def test_rule_execution_records_no_match_and_invalid_action() -> None:
    """Unmatched and malformed rules leave diagnostic execution logs."""
    unmatched = make_rule([{"field": "views", "operator": "gt", "value": 9000}])
    invalid = make_rule()
    invalid.actions_json = "[{}]"
    session = FakeSession([unmatched, invalid])

    logs = execute_content_rules(session, 7, make_content(), "manual")

    assert [log.result for log in logs] == ["no_match", "error"]


def test_sync_tag_links_trims_deduplicates_and_ignores_empty_names() -> None:
    """Tag association replacement normalizes names before insertion."""
    session = FakeSession()
    content = make_content()

    sync_tag_links(session, content, [" 舞台 ", "舞台", "", "技巧"])

    tags = [row for row in session.added if isinstance(row, Tag)]
    links = [row for row in session.added if isinstance(row, ContentTag)]
    assert [tag.name for tag in tags] == ["舞台", "技巧"]
    assert len(links) == 2
