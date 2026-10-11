# SPDX-License-Identifier: Apache-2.0
"""Add two-state content workflow, schedule owner, and rule tables."""

from alembic import op
from backend import models  # noqa: F401
from backend.database import Base
from sqlalchemy import Column, String, text

revision = "0002_content_workflows"
down_revision = "0001_initial"
branch_labels = None
depends_on = None


def upgrade():
    bind = op.get_bind()
    Base.metadata.create_all(bind=bind)
    columns = {
        column["name"] for column in bind.dialect.get_columns(bind, "schedule")
    }
    if "owner" not in columns:
        op.add_column(
            "schedule",
            Column("owner", String(100), nullable=False, server_default=""),
        )
    op.execute(
        text(
            "UPDATE content SET lifecycle = '已发布' WHERE lifecycle IN ('已发布', '已复盘', 'published', '发布')"
        )
    )
    op.execute(
        text(
            "UPDATE content SET lifecycle = '未发布' WHERE lifecycle IS NULL OR lifecycle NOT IN ('已发布', '已复盘', 'published', '发布')"
        )
    )


def downgrade():
    bind = op.get_bind()
    op.drop_column("schedule", "owner")
    Base.metadata.tables["rule_execution_log"].drop(bind=bind, checkfirst=True)
    Base.metadata.tables["content_rule"].drop(bind=bind, checkfirst=True)
