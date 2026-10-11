# SPDX-License-Identifier: Apache-2.0
"""Load and validate the project's immutable content ruleset."""

from __future__ import annotations

import json
import logging
from pathlib import Path
from typing import Any, cast

from jsonschema import Draft202012Validator
from jsonschema.exceptions import ValidationError

LOGGER = logging.getLogger(__name__)
RULESET_PATH = (
    Path(__file__).resolve().parents[2]
    / "rules"
    / "艺数智策-内容运营规则集.json"
)

RULESET_SCHEMA: dict[str, Any] = {
    "$schema": "https://json-schema.org/draft/2020-12/schema",
    "type": "object",
    "required": [
        "name",
        "version",
        "status_convention",
        "rules",
        "schedule_policy",
    ],
    "properties": {
        "name": {"type": "string", "minLength": 1},
        "version": {"type": "string", "minLength": 1},
        "description": {"type": "string"},
        "status_convention": {
            "type": "array",
            "const": ["已发布", "未发布"],
        },
        "schedule_policy": {
            "type": "object",
            "required": [
                "planning_horizon_days",
                "platforms",
                "weekdays",
                "time_windows",
                "max_per_platform_per_week",
                "max_per_day",
                "category_duration",
            ],
            "properties": {
                "planning_horizon_days": {"type": "integer", "minimum": 1},
                "platforms": {
                    "type": "array",
                    "minItems": 1,
                    "uniqueItems": True,
                    "items": {"type": "string", "minLength": 1},
                },
                "weekdays": {
                    "type": "array",
                    "minItems": 1,
                    "items": {
                        "enum": [
                            "周一",
                            "周二",
                            "周三",
                            "周四",
                            "周五",
                            "周六",
                            "周日",
                        ]
                    },
                },
                "time_windows": {
                    "type": "array",
                    "minItems": 1,
                    "items": {
                        "type": "string",
                        "pattern": "^([01][0-9]|2[0-3]):[0-5][0-9]$",
                    },
                },
                "max_per_platform_per_week": {"type": "integer", "minimum": 1},
                "max_per_day": {"type": "integer", "minimum": 1},
                "category_duration": {
                    "type": "object",
                    "minProperties": 1,
                    "additionalProperties": {
                        "type": "object",
                        "required": ["min_minutes", "max_minutes", "reason"],
                        "properties": {
                            "min_minutes": {"type": "integer", "minimum": 0},
                            "max_minutes": {"type": "integer", "minimum": 1},
                            "reason": {"type": "string", "minLength": 1},
                        },
                    },
                },
            },
            "additionalProperties": False,
        },
        "rules": {
            "type": "array",
            "items": {
                "type": "object",
                "required": [
                    "name",
                    "priority",
                    "enabled",
                    "trigger",
                    "logic",
                    "conditions",
                    "scope",
                    "actions",
                ],
                "properties": {
                    "name": {"type": "string", "minLength": 1},
                    "priority": {"type": "integer"},
                    "enabled": {"type": "boolean"},
                    "trigger": {"type": "string", "minLength": 1},
                    "logic": {"enum": ["all", "any"]},
                    "conditions": {
                        "type": "array",
                        "items": {
                            "type": "object",
                            "required": ["field", "operator", "value"],
                            "properties": {
                                "field": {
                                    "enum": [
                                        "platform",
                                        "category",
                                        "lifecycle",
                                        "views",
                                        "likes",
                                        "comments",
                                        "completion",
                                        "owner",
                                        "source",
                                    ]
                                },
                                "operator": {
                                    "enum": [
                                        "eq",
                                        "neq",
                                        "contains",
                                        "gt",
                                        "gte",
                                        "lt",
                                        "lte",
                                    ]
                                },
                                "value": {},
                            },
                            "additionalProperties": False,
                        },
                    },
                    "scope": {"type": "object"},
                    "actions": {
                        "type": "array",
                        "items": {
                            "type": "object",
                            "required": ["type", "value"],
                            "properties": {
                                "type": {
                                    "enum": [
                                        "set_status",
                                        "add_tag",
                                        "set_owner",
                                        "append_note",
                                        "set_category",
                                    ]
                                },
                                "value": {},
                            },
                            "additionalProperties": False,
                        },
                    },
                },
            },
        },
    },
}


class RuleSetError(ValueError):
    """Raised when the configured ruleset cannot be read or validated."""


def load_rule_set(path: Path = RULESET_PATH) -> dict[str, Any]:
    """Read and validate the UTF-8 JSON ruleset.

    Args:
        path: Ruleset path, primarily injectable for tests.

    Returns:
        Validated ruleset mapping.

    Raises:
        RuleSetError: If the file is missing, malformed, or schema-invalid.
    """
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
        Draft202012Validator(RULESET_SCHEMA).validate(data)
        for category, limits in data["schedule_policy"][
            "category_duration"
        ].items():
            if limits["min_minutes"] > limits["max_minutes"]:
                raise RuleSetError(f"品类 {category} 的最小时长大于最大时长")
        for rule in data["rules"]:
            for action in rule["actions"]:
                if action["type"] == "set_status" and action["value"] not in {
                    "已发布",
                    "未发布",
                }:
                    raise RuleSetError("规则状态动作只允许已发布或未发布")
    except (
        OSError,
        UnicodeError,
        json.JSONDecodeError,
        ValidationError,
    ) as exc:
        raise RuleSetError(f"规则集加载或校验失败（{path}）：{exc}") from exc
    return cast(dict[str, Any], data)


def load_rule_set_safe(path: Path = RULESET_PATH) -> dict[str, Any] | None:
    """Load rules, logging and returning no rules on configuration failure.

    Args:
        path: Ruleset path, primarily injectable for tests.

    Returns:
        The validated ruleset, or ``None`` when unavailable.
    """
    try:
        return load_rule_set(path)
    except RuleSetError as exc:
        LOGGER.error("内容规则集不可用：%s", exc)
        return None
