# SPDX-License-Identifier: Apache-2.0
"""Tests for ruleset file loading and JSON Schema validation."""

from __future__ import annotations

import json
import logging
from pathlib import Path

import pytest
from backend.scheduler.rules_loader import (
    RULESET_PATH,
    RuleSetError,
    load_rule_set,
    load_rule_set_safe,
)


def test_repository_ruleset_loads_as_utf8_and_has_schedule_policy() -> None:
    """The checked-in rule file satisfies the runtime schema."""
    ruleset = load_rule_set()

    assert RULESET_PATH.exists()
    assert ruleset["name"] == "艺数智策·内容运营规则集"
    assert ruleset["rules"]
    assert ruleset["schedule_policy"]["time_windows"]


@pytest.mark.parametrize(
    "contents",
    ["{invalid", '{"name":"invalid"}', "[]"],
)
def test_invalid_json_or_schema_raises_clear_error(
    tmp_path: Path, contents: str
) -> None:
    """Malformed and schema-invalid content raises a descriptive error."""
    path = tmp_path / "rules.json"
    path.write_text(contents, encoding="utf-8")

    with pytest.raises(RuleSetError, match="规则集加载或校验失败"):
        load_rule_set(path)


def test_missing_ruleset_falls_back_to_none_and_logs(
    tmp_path: Path, caplog
) -> None:
    """Safe loading reports a missing source without built-in fallback rules."""
    missing = tmp_path / "missing.json"
    with caplog.at_level(logging.ERROR):
        assert load_rule_set_safe(missing) is None

    assert "内容规则集不可用" in caplog.text


def test_invalid_schedule_duration_bounds_are_rejected(tmp_path: Path) -> None:
    """Cross-field duration errors are rejected beyond JSON Schema checks."""
    ruleset = load_rule_set()
    ruleset["schedule_policy"]["category_duration"]["艺术科普"][
        "min_minutes"
    ] = 7
    path = tmp_path / "rules.json"
    path.write_text(json.dumps(ruleset, ensure_ascii=False), encoding="utf-8")

    with pytest.raises(RuleSetError, match="最小时长大于最大时长"):
        load_rule_set(path)


def test_invalid_utf8_is_rejected(tmp_path: Path) -> None:
    """The loader rejects files that cannot be decoded as UTF-8."""
    path = tmp_path / "rules.json"
    path.write_bytes(b"\xff")

    with pytest.raises(RuleSetError, match="规则集加载或校验失败"):
        load_rule_set(path)
