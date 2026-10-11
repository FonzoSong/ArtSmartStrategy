# SPDX-License-Identifier: Apache-2.0
"""Explicitly inject the external 120-record example set into a team."""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_ROOT))

from backend.database import SessionLocal  # noqa: E402
from backend.example_data import load_example_data  # noqa: E402
from backend.seeding import (  # noqa: E402
    confirm_seed_over_existing,
    seed_example_team,
)


def main() -> int:
    """Load example data and require confirmation for an existing team."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--team-id", type=int, default=1)
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()
    payload = load_example_data()
    with SessionLocal() as db:
        confirmed = confirm_seed_over_existing(
            db, args.team_id, args.force, input
        )
        if not confirmed:
            print("已取消，未写入测试数据。")
            return 1
        inserted = seed_example_team(db, args.team_id, payload)
    print(f"120 条示例数据注入完成：新增 {inserted} 条内容记录。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
