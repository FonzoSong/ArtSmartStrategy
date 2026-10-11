# SPDX-License-Identifier: Apache-2.0
"""Content persistence helpers shared by API workflows."""

from __future__ import annotations

import re
import unicodedata

from sqlalchemy import select
from sqlalchemy.orm import Session

from backend.models import Content, ContentTag, Tag


def normalize_tag_name(value: str) -> tuple[str, str]:
    """Normalize display and identity forms for a user-entered tag."""
    display = unicodedata.normalize("NFKC", value).strip()
    if (
        not display
        or len(display) > 50
        or not re.fullmatch(r"[\w#-]+", display)
    ):
        raise ValueError(
            "Tag 仅允许中英文、数字、下划线、短横线和 #，长度 1–50"
        )
    return display, display.casefold()


def sync_tag_links(
    db: Session,
    item: Content,
    names: list[str],
    actor_user_id: int | None = None,
) -> None:
    """Synchronize a content record's normalized tag associations.

    Args:
        db: Active database session.
        item: Content record whose associations should be updated.
        names: Tag names selected for the record.
        actor_user_id: User creating any missing tags.
    """
    # Flush pending link rows first so rule actions can safely rebuild the set.
    db.flush()
    db.query(ContentTag).filter(ContentTag.content_id == item.id).delete(
        synchronize_session=False
    )
    selected: dict[str, str] = {}
    for raw in names:
        if not raw.strip():
            continue
        name, key = normalize_tag_name(raw)
        selected.setdefault(key, name)
    for key, name in selected.items():
        tag = db.scalar(
            select(Tag).where(
                Tag.team_id == item.team_id, Tag.normalized_name == key
            )
        )
        if not tag:
            tag = Tag(
                team_id=item.team_id,
                name=name,
                normalized_name=key,
                created_by_user_id=actor_user_id,
            )
            db.add(tag)
            db.flush()
        if tag.is_active is False:
            raise ValueError(f"Tag「{tag.name}」已停用，不能新增使用")
        db.add(ContentTag(content_id=item.id, tag_id=tag.id))
