# SPDX-License-Identifier: Apache-2.0
"""Smoke-test registration and login against PostgreSQL."""

from __future__ import annotations

import os
import sys
from pathlib import Path
from uuid import uuid4

from dotenv import load_dotenv

PROJECT_ROOT = Path(__file__).resolve().parents[1]
load_dotenv(PROJECT_ROOT / ".env")

if not os.getenv("DATABASE_URL", "").startswith("postgresql"):
    raise SystemExit("请先配置 PostgreSQL DATABASE_URL")
os.environ["SEED_ON_START"] = "false"
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from backend.database import SessionLocal  # noqa: E402
from backend.main import app  # noqa: E402
from backend.models import AuditLog, Team, User, UserSession  # noqa: E402
from fastapi.testclient import TestClient  # noqa: E402

test_email = f"auth-test-{uuid4().hex}@example.test"
team_id: int | None = None
try:
    with TestClient(app) as client:
        registration = client.post(
            "/api/auth/register-team",
            json={
                "email": test_email,
                "password": "auth-test-password",
                "name": "认证测试负责人",
                "team_name": "认证测试团队",
            },
        )
        assert registration.status_code == 200, registration.text
        team_id = registration.json()["user"]["team_id"]

        failed = client.post(
            "/api/auth/login",
            json={"email": test_email, "password": "wrong-password"},
        )
        assert failed.status_code == 401, failed.text

        login = client.post(
            "/api/auth/login",
            json={"email": test_email, "password": "auth-test-password"},
        )
        assert login.status_code == 200, login.text
        assert login.json()["user"]["role"] == "owner"
        print("PASS PostgreSQL 注册、密码校验和登录流程")
finally:
    if team_id is not None:
        with SessionLocal() as db:
            user_ids = (
                db.query(User.id).filter(User.team_id == team_id).subquery()
            )
            db.query(UserSession).filter(
                UserSession.user_id.in_(user_ids)
            ).delete(synchronize_session=False)
            db.query(AuditLog).filter(AuditLog.team_id == team_id).delete(
                synchronize_session=False
            )
            db.query(User).filter(User.team_id == team_id).delete(
                synchronize_session=False
            )
            db.query(Team).filter(Team.id == team_id).delete(
                synchronize_session=False
            )
            db.commit()
