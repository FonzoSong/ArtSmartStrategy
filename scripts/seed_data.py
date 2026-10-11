# SPDX-License-Identifier: Apache-2.0
"""Explicitly inject the versioned test fixture into a team."""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(PROJECT_ROOT))

from backend.database import SessionLocal  # noqa: E402
from backend.seeding import (  # noqa: E402
    confirm_seed_over_existing,
    load_seed_fixture,
    seed_team,
)


def main() -> int:
    """Load fixture data and require confirmation when team data exists."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--team-id", type=int, default=1)
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()
    fixture = load_seed_fixture()
    with SessionLocal() as db:
        confirmed = confirm_seed_over_existing(
            db, args.team_id, args.force, input
        )
        if not confirmed:
            print("已取消，未写入测试数据。")
            return 1
        inserted = seed_team(db, args.team_id, fixture)
    print(f"测试数据注入完成：新增 {inserted} 条。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
