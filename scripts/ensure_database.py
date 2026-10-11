# SPDX-License-Identifier: Apache-2.0
"""Create the configured PostgreSQL database when it does not exist."""

import os
import sys

import psycopg
from dotenv import load_dotenv
from psycopg import sql
from sqlalchemy.engine import make_url

load_dotenv()
database_url = os.getenv(
    "DATABASE_URL",
    "postgresql+psycopg://arts_smart:change-this@localhost:5432/arts_smart",
)
url = make_url(database_url)
if not url.drivername.startswith("postgresql") or not url.database:
    print("DATABASE_URL 必须指向 PostgreSQL，并指定数据库名。", file=sys.stderr)
    raise SystemExit(2)

try:
    with psycopg.connect(
        host=url.host or "localhost",
        port=url.port or 5432,
        user=url.username,
        password=url.password,
        dbname="postgres",
        connect_timeout=5,
        autocommit=True,
    ) as connection:
        with connection.cursor() as cursor:
            cursor.execute(
                "SELECT 1 FROM pg_database WHERE datname = %s", (url.database,)
            )
            exists = cursor.fetchone() is not None
            if not exists:
                cursor.execute(
                    sql.SQL("CREATE DATABASE {}").format(
                        sql.Identifier(url.database)
                    )
                )
except psycopg.Error as exc:
    print(
        "无法连接或创建 PostgreSQL 数据库。请确认 PostgreSQL 服务已启动，"
        "并检查 .env 中的 DATABASE_URL、账号和密码。\n"
        f"详细信息：{exc}",
        file=sys.stderr,
    )
    raise SystemExit(1)

print(f"PostgreSQL 数据库已就绪：{url.database}")
