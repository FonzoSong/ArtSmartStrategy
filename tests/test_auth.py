# SPDX-License-Identifier: Apache-2.0
"""Tests for authentication dependencies and bearer tokens."""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
from types import SimpleNamespace
from typing import Any

import pytest
from backend.auth import auth_claims, issue_token, require_editor, role, team_id
from backend.models import User, UserSession
from fastapi import HTTPException


class FakeSession:
    """In-memory session rows needed by token validation."""

    def __init__(self) -> None:
        self.rows: dict[tuple[type[Any], Any], Any] = {}
        self.added: list[Any] = []
        self.user = SimpleNamespace(
            id=4,
            team_id=9,
            role_name="owner",
            email="owner@example.test",
            name="负责人",
        )

    def add(self, row: Any) -> None:
        """Persist a fake row by its model type and identifier."""
        self.added.append(row)
        if isinstance(row, UserSession):
            self.rows[(UserSession, row.id)] = row

    def flush(self) -> None:
        """Match the database session interface."""

    def get(self, model: type[Any], key: Any) -> Any:
        """Return the configured account or session row."""
        if model is User:
            return self.user if key == self.user.id else None
        return self.rows.get((model, key))


def test_issue_token_and_authenticate_claims() -> None:
    """Issued bearer tokens resolve to the persisted account session."""
    db = FakeSession()
    token = issue_token(db, db.user)

    claims = auth_claims(f"Bearer {token}", db)

    assert claims == {
        "user_id": 4,
        "team_id": 9,
        "role": "owner",
        "email": "owner@example.test",
        "name": "负责人",
        "session_id": db.added[0].id,
    }
    assert len(db.added) == 1


@pytest.mark.parametrize(
    "authorization",
    ["", "Basic dXNlcjpwYXNz", "Bearer invalid-token", "Bearer payload.bad"],
)
def test_auth_claims_rejects_invalid_bearer_tokens(
    authorization: str,
) -> None:
    """Missing, malformed, and tampered tokens return unauthorized."""
    with pytest.raises(HTTPException) as error:
        auth_claims(authorization, FakeSession())

    assert error.value.status_code == 401


@pytest.mark.parametrize("state", ["revoked", "expired"])
def test_auth_claims_rejects_revoked_or_expired_sessions(state: str) -> None:
    """A valid signature cannot revive a revoked or expired DB session."""
    db = FakeSession()
    token = issue_token(db, db.user)
    session = db.added[0]
    if state == "revoked":
        session.revoked = True
    else:
        session.expires_at = datetime.now(timezone.utc).replace(
            tzinfo=None
        ) - timedelta(seconds=1)

    with pytest.raises(HTTPException) as error:
        auth_claims(f"Bearer {token}", db)

    assert error.value.status_code == 401


def test_team_and_role_dependencies_enforce_account_scope() -> None:
    """Team selection is constrained and editor access preserves the role."""
    claims = {"team_id": 9, "role": "editor"}

    assert team_id(claims, None) == 9
    assert team_id(claims, 9) == 9
    assert role(claims) == "editor"
    assert require_editor("editor") == "editor"
    with pytest.raises(HTTPException) as error:
        team_id(claims, 10)
    assert error.value.status_code == 403


def test_viewer_role_is_read_only() -> None:
    """Viewers receive the established forbidden response on writes."""
    with pytest.raises(HTTPException) as error:
        require_editor("viewer")

    assert error.value.status_code == 403
