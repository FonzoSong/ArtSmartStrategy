# SPDX-License-Identifier: Apache-2.0
"""Password hashing and verification primitives."""

from __future__ import annotations

import base64
import binascii
import hashlib
import hmac
import secrets


def password_hash(password: str, salt: bytes | None = None) -> str:
    """Hash a password using PBKDF2-HMAC-SHA256.

    Args:
        password: Plain-text password to hash.
        salt: Optional 16-byte salt for deterministic tests or migrations.

    Returns:
        A self-describing ``pbkdf2_sha256`` encoded hash.
    """
    salt = salt or secrets.token_bytes(16)
    digest = hashlib.pbkdf2_hmac("sha256", password.encode(), salt, 310_000)
    encoded_salt = base64.urlsafe_b64encode(salt).decode()
    encoded_digest = base64.urlsafe_b64encode(digest).decode()
    return f"pbkdf2_sha256${encoded_salt}${encoded_digest}"


def verify_password(password: str, encoded: str) -> bool:
    """Check a password against the stored PBKDF2 hash.

    Invalid or unsupported encodings return ``False``.

    Args:
        password: Candidate plain-text password.
        encoded: Stored self-describing password hash.

    Returns:
        Whether the password matches the encoded hash.
    """
    try:
        algorithm, salt, expected = encoded.split("$")
        if algorithm != "pbkdf2_sha256":
            return False
        actual = password_hash(
            password, base64.urlsafe_b64decode(salt.encode())
        ).split("$")[-1]
        return hmac.compare_digest(actual, expected)
    except (binascii.Error, TypeError, ValueError):
        return False
