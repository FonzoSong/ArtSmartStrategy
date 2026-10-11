# SPDX-License-Identifier: Apache-2.0
"""Load and validate external example data files."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any, cast

from jsonschema import Draft202012Validator, FormatChecker

from backend.settings import ROOT


def load_example_data(root: Path = ROOT) -> dict[str, Any]:
    """Load example records and check schema and cross-entity references.

    Args:
        root: Project root containing ``example/data.json`` and schema.

    Returns:
        Validated example data payload.

    Raises:
        OSError: If either external data file cannot be read.
        ValueError: If JSON or its relationships do not match the schema.
    """
    data_path = root / "example" / "data.json"
    schema_path = root / "example" / "schema.json"
    try:
        payload = json.loads(data_path.read_text(encoding="utf-8"))
        schema = json.loads(schema_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise ValueError(f"JSON 格式错误：{exc}") from exc
    errors = sorted(
        Draft202012Validator(
            schema, format_checker=FormatChecker()
        ).iter_errors(payload),
        key=lambda error: list(error.absolute_path),
    )
    if errors:
        first = errors[0]
        path = ".".join(str(part) for part in first.absolute_path) or "root"
        raise ValueError(f"{path}: {first.message}")
    teams = payload.get("teams") or [payload["team"]]
    team_ids = {int(item["id"]) for item in teams}
    users = {item["email"]: item for item in payload["users"]}
    if not any(item["role"] == "owner" for item in payload["users"]):
        raise ValueError("users 列表必须包含一名团队负责人")
    for team_id in team_ids:
        if not any(
            person["role"] == "owner"
            and int(person.get("team_id", payload["team"]["id"])) == team_id
            for person in payload["users"]
        ):
            raise ValueError(f"团队 {team_id} 缺少负责人")
    performances = {item["id"]: item for item in payload["performances"]}
    for person in payload["users"]:
        if int(person.get("team_id", payload["team"]["id"])) not in team_ids:
            raise ValueError(f"成员 {person['email']} 归属了不存在的团队")
    for performance in payload["performances"]:
        person = users.get(performance["owner_email"])
        if not person:
            raise ValueError(f"演出 {performance['id']} 的负责人不存在")
        team_id = int(
            performance.get(
                "team_id", person.get("team_id", payload["team"]["id"])
            )
        )
        if team_id not in team_ids or team_id != int(
            person.get("team_id", payload["team"]["id"])
        ):
            raise ValueError(f"演出 {performance['id']} 的团队与负责人不匹配")
    for content in payload["contents"]:
        booking = performances.get(content["performance_id"])
        person = users.get(content["owner_email"])
        if not booking:
            raise ValueError(f"内容 {content['id']} 关联了不存在的演出")
        if not person:
            raise ValueError(f"内容 {content['id']} 的负责人不存在")
        team_id = int(
            content.get("team_id", person.get("team_id", payload["team"]["id"]))
        )
        booking_team = int(booking.get("team_id", payload["team"]["id"]))
        person_team = int(person.get("team_id", payload["team"]["id"]))
        if team_id != booking_team or team_id != person_team:
            raise ValueError(
                f"内容 {content['id']} 的团队、演出、负责人关联不一致"
            )
    content_rows = {row["id"]: row for row in payload["contents"]}
    for schedule in payload.get("schedules", []):
        content = content_rows.get(schedule["content_id"])
        person = users.get(schedule["owner_email"])
        if not content or not person:
            raise ValueError("排期记录引用了不存在的内容或负责人")
        content_team = int(content.get("team_id", payload["team"]["id"]))
        person_team = int(person.get("team_id", payload["team"]["id"]))
        if content_team != person_team:
            raise ValueError("排期、内容和负责人不属于同一团队")
    return cast(dict[str, Any], payload)
