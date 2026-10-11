# SPDX-License-Identifier: Apache-2.0
"""Drop and recreate the configured PostgreSQL public schema."""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

import psycopg
from dotenv import load_dotenv
from sqlalchemy.engine import make_url

ROOT = Path(__file__).resolve().parents[1]
load_dotenv(ROOT / ".env")
raw = os.getenv("DATABASE_URL", "")
if not raw.startswith("postgresql"):
    raise SystemExit("DATABASE_URL 必须使用 PostgreSQL，拒绝重置 SQLite 数据库")
url = make_url(raw)
if not url.database:
    raise SystemExit("DATABASE_URL 缺少数据库名")

answer = input(
    f"将清空 PostgreSQL 数据库 {url.database} 的 public schema，继续请输入 RESET："
)
if answer.strip() != "RESET":
    raise SystemExit("已取消")

with psycopg.connect(
    host=url.host or "localhost",
    port=url.port or 5432,
    user=url.username,
    password=url.password,
    dbname=url.database,
    connect_timeout=10,
    autocommit=True,
) as conn:
    with conn.cursor() as cur:
        cur.execute("DROP SCHEMA public CASCADE")
        cur.execute("CREATE SCHEMA public")

env = os.environ.copy()
subprocess.run(
    [sys.executable, "-m", "alembic", "upgrade", "head"],
    cwd=ROOT,
    env=env,
    check=True,
)
print(
    "数据库已清空并完成 Alembic 迁移；启动后为空库，请通过 Web 创建团队账号。"
)
