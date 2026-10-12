# SPDX-License-Identifier: Apache-2.0
"""Environment-backed settings shared by the application and migrations."""

from __future__ import annotations

import hashlib
import os
import secrets
import tempfile
import time
from pathlib import Path

from dotenv import load_dotenv

ROOT = Path(__file__).resolve().parents[1]
load_dotenv(ROOT / ".env")

DEFAULT_APP_SECRET = "local-development-secret-change-before-network-use"


def ensure_app_secret(env_path: Path | None = None) -> str:
    """Create and persist a cryptographically strong secret when needed.

    Args:
        env_path: Optional dotenv file path. Defaults to the project ``.env``.

    Returns:
        The existing valid secret or a newly generated secret.

    Raises:
        RuntimeError: If a new secret cannot be persisted safely.
    """
    current = os.getenv("APP_SECRET", "").strip()
    if (
        current
        and current != DEFAULT_APP_SECRET
        and current != "replace-this-for-a-shared-deployment"
    ):
        return current

    configured_path = os.getenv("APP_SECRET_FILE")
    path = env_path or Path(configured_path or ROOT / ".env")
    try:
        lines = (
            path.read_text(encoding="utf-8").splitlines()
            if path.exists()
            else []
        )
    except OSError as exc:
        raise RuntimeError(
            f"无法读取 APP_SECRET 存储文件 {path}: {exc}"
        ) from exc
    stored_secret = next(
        (
            line.split("=", maxsplit=1)[1].strip().strip("\"'")
            for line in lines
            if line.lstrip().startswith("APP_SECRET=")
        ),
        "",
    )
    if (
        stored_secret
        and stored_secret != DEFAULT_APP_SECRET
        and stored_secret != "replace-this-for-a-shared-deployment"
    ):
        os.environ["APP_SECRET"] = stored_secret
        return stored_secret
    try:
        now = str(time.time_ns()).encode("ascii")
        secret = hashlib.sha256(now + secrets.token_bytes(64)).hexdigest()
        updated = [
            line
            for line in lines
            if not line.lstrip().startswith("APP_SECRET=")
        ]
        updated.append(f"APP_SECRET={secret}")
        path.parent.mkdir(parents=True, exist_ok=True)
        fd, temp_name = tempfile.mkstemp(prefix=".env.", dir=path.parent)
        try:
            with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as stream:
                stream.write("\n".join(updated) + "\n")
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(temp_name, path)
        finally:
            if os.path.exists(temp_name):
                os.unlink(temp_name)
        os.environ["APP_SECRET"] = secret
        return secret
    except OSError as exc:
        raise RuntimeError(
            f"无法安全初始化 APP_SECRET，请检查 {path} 的写入权限: {exc}"
        ) from exc


APP_SECRET = ensure_app_secret()

DATA_DIR = ROOT / "data"
DATA_DIR.mkdir(exist_ok=True)

configured_database_url = os.getenv("DATABASE_URL")
DATABASE_CONFIGURED = bool(
    configured_database_url and configured_database_url.strip()
)
DATABASE_URL = configured_database_url or (
    "postgresql+psycopg://arts_smart:change-this@localhost:5432/arts_smart"
)
AI_BASE_URL = os.getenv("AI_BASE_URL", "https://api.kunyou.asia/v1").rstrip("/")
AI_MODEL = os.getenv("AI_MODEL", "qwen3.8-max")
if not DATABASE_URL.startswith("postgresql"):
    raise RuntimeError(
        "DATABASE_URL 必须使用 PostgreSQL（postgresql+psycopg://...）"
    )

MIN_SAMPLE_DEFAULT = max(2, int(os.getenv("MIN_SAMPLE_SIZE", "5")))
CATEGORIES = ["演出宣传", "技艺展示", "幕后记录", "艺术科普"]
PLATFORMS = ["短视频平台A", "短视频平台B", "内容社区C", "视频号"]
