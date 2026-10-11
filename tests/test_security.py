# SPDX-License-Identifier: Apache-2.0
"""Tests for password hashing compatibility."""

import pytest
from backend.security import password_hash, verify_password


def test_password_hash_is_deterministic_with_explicit_salt() -> None:
    """An explicit salt supports stable migration and regression checks."""
    salt = b"0123456789abcdef"
    encoded = password_hash("correct horse battery staple", salt)

    assert encoded == password_hash("correct horse battery staple", salt)
    assert encoded.startswith("pbkdf2_sha256$")


def test_password_hash_uses_random_salts_by_default() -> None:
    """Default password hashes use independent salts."""
    first = password_hash("correct horse battery staple")
    second = password_hash("correct horse battery staple")

    assert first != second
    assert verify_password("correct horse battery staple", first)


def test_password_verification_rejects_wrong_password_and_bad_hash() -> None:
    """Wrong passwords and malformed encodings fail without raising."""
    valid = password_hash("correct horse battery staple")

    assert not verify_password("wrong password", valid)
    assert not verify_password("password", "not-a-valid-hash")
    assert not verify_password("password", "pbkdf2_sha256$%%%$hash")


@pytest.mark.parametrize("encoded", ["", "pbkdf2_sha1$a$b", "a$b$c$d"])
def test_password_verification_rejects_unsupported_format(
    encoded: str,
) -> None:
    """Unsupported algorithms and malformed component counts are rejected."""
    assert not verify_password("password", encoded)
