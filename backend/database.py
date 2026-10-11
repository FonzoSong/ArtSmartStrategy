# SPDX-License-Identifier: Apache-2.0
"""SQLAlchemy base, PostgreSQL engine, and request-scoped sessions."""

from __future__ import annotations

from collections.abc import Iterator

from sqlalchemy import create_engine
from sqlalchemy.orm import DeclarativeBase, Session, sessionmaker

from backend.settings import DATABASE_URL


class Base(DeclarativeBase):
    """Base class for SQLAlchemy models."""


engine = create_engine(DATABASE_URL, future=True)
SessionLocal = sessionmaker(
    bind=engine, autoflush=False, expire_on_commit=False
)


def get_db() -> Iterator[Session]:
    """Yield a database session and close it after the request.

    Yields:
        A SQLAlchemy session connected to the configured PostgreSQL database.
    """
    db = SessionLocal()
    try:
        yield db
    finally:
        db.close()
