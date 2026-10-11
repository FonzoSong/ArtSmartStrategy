# SPDX-License-Identifier: Apache-2.0
"""Secure APP_SECRET initialization behavior."""

from __future__ import annotations

from pathlib import Path

import pytest
from backend.settings import DEFAULT_APP_SECRET, ensure_app_secret


def test_missing_secret_is_generated_and_persisted(
    tmp_path: Path, monkeypatch
) -> None:
    """A missing key is persisted and reused on the next initialization."""
    env_file = tmp_path / ".env"
    env_file.write_text("DATABASE_URL=postgresql://local\n", encoding="utf-8")
    monkeypatch.delenv("APP_SECRET", raising=False)

    generated = ensure_app_secret(env_file)
    assert len(generated) == 64
    assert generated in env_file.read_text(encoding="utf-8")
    assert ensure_app_secret(env_file) == generated


def test_placeholder_is_replaced_but_existing_secret_is_preserved(
    tmp_path: Path, monkeypatch
) -> None:
    """Only documented placeholders are rotated automatically."""
    env_file = tmp_path / ".env"
    env_file.write_text(f"APP_SECRET={DEFAULT_APP_SECRET}\n", encoding="utf-8")
    monkeypatch.setenv("APP_SECRET", DEFAULT_APP_SECRET)
    generated = ensure_app_secret(env_file)
    assert generated != DEFAULT_APP_SECRET
    assert ensure_app_secret(env_file) == generated


def test_persisted_secret_store_reuses_secret_when_environment_is_placeholder(
    tmp_path: Path, monkeypatch
) -> None:
    """A directory-mounted Docker secret file survives container recreation."""
    secret_file = tmp_path / "runtime-secret" / ".env"
    monkeypatch.delenv("APP_SECRET", raising=False)

    generated = ensure_app_secret(secret_file)
    monkeypatch.setenv("APP_SECRET", "replace-this-for-a-shared-deployment")

    assert ensure_app_secret(secret_file) == generated
    assert secret_file.read_text(encoding="utf-8").count("APP_SECRET=") == 1


def test_secret_write_failure_is_explicit(tmp_path: Path, monkeypatch) -> None:
    """Atomic persistence failures do not silently continue with a placeholder."""
    env_file = tmp_path / "missing-parent" / ".env"
    monkeypatch.delenv("APP_SECRET", raising=False)
    monkeypatch.setattr(
        "backend.settings.os.replace",
        lambda *_: (_ for _ in ()).throw(OSError("denied")),
    )

    with pytest.raises(RuntimeError, match="无法安全初始化 APP_SECRET"):
        ensure_app_secret(env_file)
