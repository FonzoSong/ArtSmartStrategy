# SPDX-License-Identifier: Apache-2.0
"""Tests for executing rules from the read-only project ruleset."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from backend.content import sync_tag_links
from backend.models import Content, ContentTag, RuleExecutionLog, Tag
from backend.rules import execute_content_rules, rule_matches
from backend.scheduler.rules_loader import load_rule_set


class FakeQuery:
    """Query stub used by tag-link synchronization."""

    def filter(self, *_args: Any, **_kwargs: Any) -> FakeQuery:
        """Keep the query adapter chainable."""
        return self

    def delete(self, **_kwargs: Any) -> int:
        """Report that there are no prior association rows."""
        return 0


class FakeSession:
    """Collect rows produced during rule execution."""

    def __init__(self) -> None:
        self.added: list[Any] = []
        self.next_tag_id = 1

    def query(self, *_args: Any, **_kwargs: Any) -> FakeQuery:
        """Return the query stub used by the tag helper."""
        return FakeQuery()

    def flush(self) -> None:
        """Match the SQLAlchemy session flush interface."""

    def scalar(self, *_args: Any, **_kwargs: Any) -> None:
        """Report that a matching tag does not already exist."""
        return None

    def add(self, item: Any) -> None:
        """Collect database rows and assign tag identifiers."""
        if isinstance(item, Tag):
            item.id = self.next_tag_id
            self.next_tag_id += 1
        self.added.append(item)


def make_content(**overrides: Any) -> Content:
    """Build content with stable attributes for rule matching."""
    values: dict[str, Any] = {
        "id": "content-1",
        "team_id": 7,
        "title": "练习中的舞台动作",
        "category": "幕后记录",
        "platform": "短视频平台A",
        "published_at": datetime(2026, 10, 11, 19, 30),
        "duration": 48,
        "views": 1200,
        "likes": 120,
        "comments": 30,
        "saves": 10,
        "shares": 5,
        "completion": 65.0,
        "lifecycle": "已发布",
        "owner": "小林",
        "source": "manual",
        "tags_json": "[]",
        "notes": "已有备注",
    }
    values.update(overrides)
    return Content(**values)


def test_file_rule_matches_published_performance_conditions() -> None:
    """The configured high-performance rule matches its published criteria."""
    ruleset = load_rule_set()
    rule = next(
        item
        for item in ruleset["rules"]
        if item["name"] == "高表现内容进入复盘池"
    )

    assert rule_matches(rule, make_content())
    assert not rule_matches(rule, make_content(views=100, completion=10))


def test_static_create_rule_applies_file_actions_and_logs_match() -> None:
    """The JSON-defined create rule applies tags and notes without DB rules."""
    content = make_content()
    db = FakeSession()

    logs = execute_content_rules(db, 7, content, "content.create")

    matched = [log for log in logs if log.result == "matched"]
    assert [log.rule_name for log in matched] == ["高表现内容进入复盘池"]
    assert matched[0].rule_id is None
    assert "高表现待复盘" in content.tags_json
    assert any(isinstance(row, ContentTag) for row in db.added)
    assert any(isinstance(row, RuleExecutionLog) for row in db.added)


def test_static_unpublished_rules_cover_manual_and_no_match_paths() -> None:
    """Unpublished governance matches drafts and published content does not."""
    draft = make_content(lifecycle="未发布", owner="")
    db = FakeSession()

    logs = execute_content_rules(db, 7, draft, "manual")

    assert len(logs) == 1
    assert logs[0].result == "matched"
    assert "未发布资产" in draft.tags_json
    assert "继续制作" in draft.notes

    unmatched = execute_content_rules(
        FakeSession(), 7, make_content(views=1), "content.create"
    )
    assert unmatched[0].result == "no_match"


def test_missing_rule_set_returns_no_execution_rows(monkeypatch) -> None:
    """An unavailable rule file safely disables rule execution."""
    monkeypatch.setattr("backend.rules.load_rule_set_safe", lambda: None)
    assert (
        execute_content_rules(FakeSession(), 7, make_content(), "manual") == []
    )


def test_sync_tag_links_trims_deduplicates_and_ignores_empty_names() -> None:
    """Normalize names before saving tag relationships."""
    db = FakeSession()
    sync_tag_links(db, make_content(), [" 舞台 ", "舞台", "", "技巧"])

    tags = [row for row in db.added if isinstance(row, Tag)]
    links = [row for row in db.added if isinstance(row, ContentTag)]
    assert [tag.name for tag in tags] == ["舞台", "技巧"]
    assert len(links) == 2
