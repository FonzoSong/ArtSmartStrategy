# SPDX-License-Identifier: Apache-2.0
"""Authentication dependencies and signed session token helpers."""

from __future__ import annotations

import base64
import binascii
import hashlib
import hmac
import json
import uuid
from datetime import datetime, timedelta, timezone
from typing import Any, cast

from fastapi import Depends, Header, HTTPException
from sqlalchemy.orm import Session

from backend.database import get_db
from backend.models import User, UserSession
from backend.settings import APP_SECRET


def _utc_now() -> datetime:
    """Return the current UTC time as a naive datetime for DB compatibility."""
    return datetime.now(timezone.utc).replace(tzinfo=None)


def issue_token(db: Session, user: User) -> str:
    """Create and persist a signed two-week login session.

    Args:
        db: Active database session.
        user: Account receiving the session.

    Returns:
        A URL-safe token containing the session and account identifiers.
    """
    sid = str(uuid.uuid4())
    expires = _utc_now() + timedelta(days=14)
    db.add(UserSession(id=sid, user_id=user.id, expires_at=expires))
    db.flush()
    payload = (
        base64.urlsafe_b64encode(
            json.dumps(
                {
                    "sid": sid,
                    "sub": user.id,
                    "tid": user.team_id,
                    "exp": int(expires.timestamp()),
                },
                separators=(",", ":"),
            ).encode()
        )
        .decode()
        .rstrip("=")
    )
    signature = hmac.new(
        APP_SECRET.encode(), payload.encode(), hashlib.sha256
    ).hexdigest()
    return payload + "." + signature


def auth_claims(
    authorization: str = Header(default=""), db: Session = Depends(get_db)
) -> dict[str, Any]:
    """Validate a bearer token and return its authenticated account claims.

    Args:
        authorization: HTTP Authorization header.
        db: Active database session.

    Returns:
        Account, team, and session identifiers for downstream dependencies.

    Raises:
        HTTPException: If the token is absent, malformed, expired, or revoked.
    """
    if not authorization.startswith("Bearer "):
        raise HTTPException(401, "请先登录后继续")
    token = authorization[7:].strip()
    try:
        payload, signature = token.split(".", 1)
        expected = hmac.new(
            APP_SECRET.encode(), payload.encode(), hashlib.sha256
        ).hexdigest()
        if not hmac.compare_digest(signature, expected):
            raise ValueError("签名无效")
        decoded = json.loads(
            base64.urlsafe_b64decode(
                payload + "=" * ((4 - len(payload) % 4) % 4)
            )
        )
        if int(decoded["exp"]) < int(_utc_now().timestamp()):
            raise ValueError("登录已过期")
        session = db.get(UserSession, decoded["sid"])
        user = db.get(User, decoded["sub"])
        if (
            not session
            or session.revoked
            or not user
            or session.expires_at < _utc_now()
        ):
            raise ValueError("登录状态已失效")
        return {
            "user_id": user.id,
            "team_id": user.team_id,
            "role": user.role_name,
            "email": user.email,
            "name": user.name,
            "session_id": session.id,
        }
    except HTTPException:
        raise
    except (
        binascii.Error,
        json.JSONDecodeError,
        KeyError,
        TypeError,
        ValueError,
    ):
        raise HTTPException(401, "登录状态无效或已过期")


def team_id(
    claims: dict[str, Any] = Depends(auth_claims),
    x_team_id: int | None = Header(default=None, alias="X-Team-Id"),
) -> int:
    """Return the authenticated account's team after header validation.

    Args:
        claims: Claims created by ``auth_claims``.
        x_team_id: Optional team selector supplied by the client.

    Returns:
        The authenticated account's team identifier.

    Raises:
        HTTPException: If the client selects a different team.
    """
    if x_team_id is not None and x_team_id != claims["team_id"]:
        raise HTTPException(403, "当前账号无权访问该团队")
    return cast(int, claims["team_id"])


def role(claims: dict[str, Any] = Depends(auth_claims)) -> str:
    """Return the authenticated account's role name."""
    return cast(str, claims["role"])


def require_editor(user_role: str = Depends(role)) -> str:
    """Reject viewer accounts from write operations.

    Args:
        user_role: Role returned by the authenticated account dependency.

    Returns:
        The role for endpoint compatibility when writes are allowed.

    Raises:
        HTTPException: If the caller has the read-only viewer role.
    """
    if user_role == "viewer":
        raise HTTPException(403, "查看者账号不能修改内容")
    return user_role
