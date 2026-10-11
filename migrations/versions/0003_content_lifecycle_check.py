# SPDX-License-Identifier: Apache-2.0
"""Constrain content status to the two supported lifecycle values."""

from alembic import op
from sqlalchemy import text

revision = "0003_content_lifecycle_check"
down_revision = "0002_content_workflows"
branch_labels = None
depends_on = None


def upgrade() -> None:
    """Normalize legacy rows, then enforce the two-state constraint."""
    bind = op.get_bind()
    op.execute(
        text(
            "UPDATE content SET lifecycle = '已发布' "
            "WHERE lifecycle IN ('已发布', '已复盘', 'published', '发布')"
        )
    )
    op.execute(
        text(
            "UPDATE content SET lifecycle = '未发布' "
            "WHERE lifecycle IS NULL OR lifecycle NOT IN ('已发布', '已复盘', 'published', '发布')"
        )
    )
    existing = {
        item["name"]
        for item in bind.dialect.get_check_constraints(bind, "content")
    }
    if "ck_content_lifecycle_two_states" not in existing:
        op.create_check_constraint(
            "ck_content_lifecycle_two_states",
            "content",
            "lifecycle IN ('已发布', '未发布')",
        )


def downgrade() -> None:
    """Remove the lifecycle check constraint."""
    existing = {
        item["name"]
        for item in op.get_bind().dialect.get_check_constraints(
            op.get_bind(), "content"
        )
    }
    if "ck_content_lifecycle_two_states" in existing:
        op.drop_constraint(
            "ck_content_lifecycle_two_states", "content", type_="check"
        )
