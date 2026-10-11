# SPDX-License-Identifier: Apache-2.0
"""Environment-backed settings shared by the application and migrations."""

from __future__ import annotations

import os
from pathlib import Path

from dotenv import load_dotenv

ROOT = Path(__file__).resolve().parents[1]
load_dotenv(ROOT / ".env")

DATA_DIR = ROOT / "data"
DATA_DIR.mkdir(exist_ok=True)

APP_SECRET = os.getenv(
    "APP_SECRET", "local-development-secret-change-before-network-use"
)
DATABASE_URL = os.getenv(
    "DATABASE_URL",
    "postgresql+psycopg://arts_smart:change-this@localhost:5432/arts_smart",
)
if not DATABASE_URL.startswith("postgresql"):
    raise RuntimeError(
        "DATABASE_URL 必须使用 PostgreSQL（postgresql+psycopg://...）"
    )

MIN_SAMPLE_DEFAULT = max(2, int(os.getenv("MIN_SAMPLE_SIZE", "5")))
CATEGORIES = ["演出宣传", "技艺展示", "幕后记录", "艺术科普"]
PLATFORMS = ["短视频平台A", "短视频平台B", "内容社区C", "视频号"]
