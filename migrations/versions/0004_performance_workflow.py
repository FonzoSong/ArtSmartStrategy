# SPDX-License-Identifier: Apache-2.0
"""Add booked performances and explicit content workflow ownership.

Revision ID: 0004_performance_workflow
Revises: 0003_content_lifecycle_check
"""

import sqlalchemy as sa
from alembic import op
from sqlalchemy import inspect

revision = "0004_performance_workflow"
down_revision = "0003_content_lifecycle_check"
branch_labels = None
depends_on = None


def upgrade() -> None:
    """Create performance bookings and additive content workflow columns."""
    bind = op.get_bind()
    inspector = inspect(bind)
    tables = set(inspector.get_table_names())
    if "performance_booking" not in tables:
        op.create_table(
            "performance_booking",
            sa.Column("id", sa.String(length=36), primary_key=True),
            sa.Column(
                "team_id",
                sa.Integer(),
                sa.ForeignKey("team.id"),
                nullable=False,
            ),
            sa.Column("title", sa.String(length=300), nullable=False),
            sa.Column(
                "venue",
                sa.String(length=200),
                nullable=False,
                server_default="",
            ),
            sa.Column("starts_at", sa.DateTime(), nullable=False),
            sa.Column(
                "booking_status",
                sa.String(length=30),
                nullable=False,
                server_default="已确认",
            ),
            sa.Column(
                "owner_user_id",
                sa.Integer(),
                sa.ForeignKey("user_account.id"),
                nullable=True,
            ),
            sa.Column("notes", sa.Text(), nullable=False, server_default=""),
            sa.Column(
                "created_at",
                sa.DateTime(),
                nullable=False,
                server_default=sa.func.now(),
            ),
        )
    content_columns = {
        item["name"] for item in inspector.get_columns("content")
    }
    if "performance_id" not in content_columns:
        op.add_column(
            "content",
            sa.Column("performance_id", sa.String(length=36), nullable=True),
        )
        op.create_foreign_key(
            "fk_content_performance_id",
            "content",
            "performance_booking",
            ["performance_id"],
            ["id"],
        )
    if "owner_user_id" not in content_columns:
        op.add_column(
            "content", sa.Column("owner_user_id", sa.Integer(), nullable=True)
        )
        op.create_foreign_key(
            "fk_content_owner_user_id",
            "content",
            "user_account",
            ["owner_user_id"],
            ["id"],
        )
    if "workflow_state" not in content_columns:
        op.add_column(
            "content",
            sa.Column(
                "workflow_state",
                sa.String(length=20),
                nullable=False,
                server_default="待制作",
            ),
        )
        op.execute(
            sa.text(
                "UPDATE content SET workflow_state = CASE "
                "WHEN lifecycle = '已发布' THEN '已发布' "
                "WHEN adopted THEN '已排期' ELSE '待排期' END"
            )
        )
    inspector = inspect(bind)
    for name, columns in (
        ("performance_booking", ("team_id", "starts_at", "owner_user_id")),
        ("content", ("performance_id", "owner_user_id", "workflow_state")),
    ):
        existing_indexes = {
            item["name"] for item in inspector.get_indexes(name)
        }
        for column in columns:
            index_name = f"ix_{name}_{column}"
            if index_name not in existing_indexes:
                op.create_index(index_name, name, [column])
    checks = {
        item["name"] for item in inspect(bind).get_check_constraints("content")
    }
    if "ck_content_workflow_state_values" not in checks:
        op.create_check_constraint(
            "ck_content_workflow_state_values",
            "content",
            "workflow_state IN ('待制作', '待排期', '已排期', '已发布')",
        )


def downgrade() -> None:
    """Remove the additive workflow and booking schema."""
    bind = op.get_bind()
    inspector = inspect(bind)
    if "content" in inspector.get_table_names():
        columns = {item["name"] for item in inspector.get_columns("content")}
        constraints = {
            item["name"] for item in inspector.get_foreign_keys("content")
        }
        checks = {
            item["name"] for item in inspector.get_check_constraints("content")
        }
        indexes = {item["name"] for item in inspector.get_indexes("content")}
        for column in ("workflow_state", "owner_user_id", "performance_id"):
            index = f"ix_content_{column}"
            if index in indexes:
                op.drop_index(index, table_name="content")
        for name in ("fk_content_owner_user_id", "fk_content_performance_id"):
            if name in constraints:
                op.drop_constraint(name, "content", type_="foreignkey")
        if "ck_content_workflow_state_values" in checks:
            op.drop_constraint(
                "ck_content_workflow_state_values", "content", type_="check"
            )
        for column in ("workflow_state", "owner_user_id", "performance_id"):
            if column in columns:
                op.drop_column("content", column)
    if "performance_booking" in inspector.get_table_names():
        indexes = {
            item["name"]
            for item in inspector.get_indexes("performance_booking")
        }
        for column in ("owner_user_id", "starts_at", "team_id"):
            index = f"ix_performance_booking_{column}"
            if index in indexes:
                op.drop_index(index, table_name="performance_booking")
        op.drop_table("performance_booking")
