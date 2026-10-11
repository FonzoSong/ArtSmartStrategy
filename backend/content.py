# SPDX-License-Identifier: Apache-2.0
"""Content persistence helpers shared by API workflows."""

from __future__ import annotations

from sqlalchemy import select
from sqlalchemy.orm import Session

from backend.models import Content, ContentTag, Tag


def sync_tag_links(db: Session, item: Content, names: list[str]) -> None:
    """Synchronize a content record's normalized tag associations.

    Args:
        db: Active database session.
        item: Content record whose associations should be updated.
        names: Tag names selected for the record.
    """
    # Flush pending link rows first so rule actions can safely rebuild the set.
    db.flush()
    db.query(ContentTag).filter(ContentTag.content_id == item.id).delete(
        synchronize_session=False
    )
    for name in dict.fromkeys(x.strip() for x in names if x.strip()):
        tag = db.scalar(
            select(Tag).where(Tag.team_id == item.team_id, Tag.name == name)
        )
        if not tag:
            tag = Tag(team_id=item.team_id, name=name)
            db.add(tag)
            db.flush()
        db.add(ContentTag(content_id=item.id, tag_id=tag.id))
