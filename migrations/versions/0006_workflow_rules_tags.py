# SPDX-License-Identifier: Apache-2.0
"""Add workflow history, editable rules, and managed tag metadata.

Revision ID: 0006_workflow_rules_tags
Revises: 0005_recommendation_status
"""

import unicodedata

import sqlalchemy as sa
from alembic import op
from sqlalchemy import inspect

revision = "0006_workflow_rules_tags"
down_revision = "0005_recommendation_status"
branch_labels = None
depends_on = None


def _columns(table: str) -> set[str]:
    return {item["name"] for item in inspect(op.get_bind()).get_columns(table)}


def upgrade() -> None:
    """Extend existing tables without rewriting historical business rows."""
    bind = op.get_bind()
    checks = {
        item["name"] for item in inspect(bind).get_check_constraints("content")
    }
    if "ck_content_workflow_state_values" in checks:
        op.drop_constraint(
            "ck_content_workflow_state_values", "content", type_="check"
        )
    op.create_check_constraint(
        "ck_content_workflow_state_values",
        "content",
        "workflow_state IN ('待制作', '已制作', '待排期', '已排期', '已发布', '未发布')",
    )
    if "cover_url" not in _columns("content"):
        op.add_column(
            "content",
            sa.Column(
                "cover_url", sa.Text(), nullable=False, server_default=""
            ),
        )

    tag_columns = _columns("tag")
    additions = (
        ("normalized_name", sa.String(100), ""),
        ("is_active", sa.Boolean(), True),
        ("created_by_user_id", sa.Integer(), None),
        ("created_at", sa.DateTime(), sa.func.now()),
    )
    for name, column_type, default in additions:
        if name not in tag_columns:
            kwargs = {"nullable": name == "created_by_user_id"}
            if default is not None:
                kwargs["server_default"] = (
                    sa.true() if default is True else str(default)
                )
            op.add_column("tag", sa.Column(name, column_type, **kwargs))
    tag_fks = inspect(bind).get_foreign_keys("tag")
    if not any(
        fk.get("constrained_columns") == ["created_by_user_id"]
        for fk in tag_fks
    ):
        op.create_foreign_key(
            "fk_tag_created_by_user_id",
            "tag",
            "user_account",
            ["created_by_user_id"],
            ["id"],
        )
    tag_rows = bind.execute(
        sa.text("SELECT id, team_id, name FROM tag")
    ).mappings()
    seen: set[tuple[int, str]] = set()
    for row in tag_rows:
        normalized = (
            unicodedata.normalize("NFKC", row["name"]).strip().casefold()
        )
        key = (row["team_id"], normalized)
        if key in seen:
            normalized = f"{normalized}#legacy-{row['id']}"
        seen.add((row["team_id"], normalized))
        bind.execute(
            sa.text(
                "UPDATE tag SET normalized_name = :normalized WHERE id = :id"
            ),
            {"normalized": normalized, "id": row["id"]},
        )
    op.alter_column("tag", "normalized_name", nullable=False)
    if "uq_tag_team_normalized_name" not in {
        item["name"] for item in inspect(bind).get_unique_constraints("tag")
    }:
        op.create_unique_constraint(
            "uq_tag_team_normalized_name", "tag", ["team_id", "normalized_name"]
        )

    rule_columns = _columns("content_rule")
    for name, column_type, nullable, default in (
        ("rule_type", sa.String(40), False, "required"),
        ("rule_content", sa.Text(), False, "{}"),
        ("content_type", sa.String(40), False, ""),
        ("workflow_state", sa.String(20), False, ""),
        ("notes", sa.Text(), False, ""),
        ("created_by_user_id", sa.Integer(), True, None),
        ("updated_by_user_id", sa.Integer(), True, None),
    ):
        if name not in rule_columns:
            op.add_column(
                "content_rule",
                sa.Column(
                    name,
                    column_type,
                    nullable=nullable,
                    **(
                        {"server_default": default}
                        if default is not None
                        else {}
                    ),
                ),
            )
    rule_fks = inspect(bind).get_foreign_keys("content_rule")
    for name in ("created_by_user_id", "updated_by_user_id"):
        fk_name = f"fk_content_rule_{name}"
        if not any(fk.get("constrained_columns") == [name] for fk in rule_fks):
            op.create_foreign_key(
                fk_name, "content_rule", "user_account", [name], ["id"]
            )

    schedule_columns = _columns("schedule")
    if "source" not in schedule_columns:
        op.add_column(
            "schedule",
            sa.Column(
                "source",
                sa.String(20),
                nullable=False,
                server_default="automatic",
            ),
        )
    if "created_by_user_id" not in schedule_columns:
        op.add_column(
            "schedule",
            sa.Column("created_by_user_id", sa.Integer(), nullable=True),
        )
    if not any(
        fk.get("constrained_columns") == ["created_by_user_id"]
        for fk in inspect(bind).get_foreign_keys("schedule")
    ):
        op.create_foreign_key(
            "fk_schedule_created_by_user_id",
            "schedule",
            "user_account",
            ["created_by_user_id"],
            ["id"],
        )
    if "updated_at" not in schedule_columns:
        op.add_column(
            "schedule",
            sa.Column(
                "updated_at",
                sa.DateTime(),
                nullable=False,
                server_default=sa.func.now(),
            ),
        )

    if "content_workflow_event" not in inspect(bind).get_table_names():
        op.create_table(
            "content_workflow_event",
            sa.Column("id", sa.Integer(), primary_key=True),
            sa.Column(
                "team_id",
                sa.Integer(),
                sa.ForeignKey("team.id"),
                nullable=False,
            ),
            sa.Column(
                "content_id",
                sa.String(36),
                sa.ForeignKey("content.id", ondelete="CASCADE"),
                nullable=False,
            ),
            sa.Column("from_state", sa.String(20), nullable=False),
            sa.Column("to_state", sa.String(20), nullable=False),
            sa.Column("source", sa.String(20), nullable=False),
            sa.Column(
                "actor_user_id",
                sa.Integer(),
                sa.ForeignKey("user_account.id"),
                nullable=True,
            ),
            sa.Column(
                "created_at",
                sa.DateTime(),
                nullable=False,
                server_default=sa.func.now(),
            ),
        )
        op.create_index(
            "ix_content_workflow_event_team_id",
            "content_workflow_event",
            ["team_id"],
        )
        op.create_index(
            "ix_content_workflow_event_content_id",
            "content_workflow_event",
            ["content_id"],
        )


def downgrade() -> None:
    """Remove new metadata and restore the previous workflow constraint."""
    bind = op.get_bind()
    if "content_workflow_event" in inspect(bind).get_table_names():
        op.drop_table("content_workflow_event")
    if "cover_url" in _columns("content"):
        op.drop_column("content", "cover_url")
    schedule_columns = _columns("schedule")
    if "created_by_user_id" in schedule_columns:
        for foreign_key in inspect(bind).get_foreign_keys("schedule"):
            if foreign_key.get("constrained_columns") == ["created_by_user_id"]:
                op.drop_constraint(
                    foreign_key["name"], "schedule", type_="foreignkey"
                )
        op.drop_column("schedule", "created_by_user_id")
    for name in ("updated_at", "source"):
        if name in _columns("schedule"):
            op.drop_column("schedule", name)
    for name in ("created_by_user_id", "updated_by_user_id"):
        for foreign_key in inspect(bind).get_foreign_keys("content_rule"):
            if foreign_key.get("constrained_columns") == [name]:
                op.drop_constraint(
                    foreign_key["name"], "content_rule", type_="foreignkey"
                )
    for name in (
        "rule_type",
        "rule_content",
        "content_type",
        "workflow_state",
        "notes",
        "created_by_user_id",
        "updated_by_user_id",
    ):
        if name in _columns("content_rule"):
            op.drop_column("content_rule", name)
    if "uq_tag_team_normalized_name" in {
        c["name"] for c in inspect(bind).get_unique_constraints("tag")
    }:
        op.drop_constraint("uq_tag_team_normalized_name", "tag", type_="unique")
    for foreign_key in inspect(bind).get_foreign_keys("tag"):
        if foreign_key.get("constrained_columns") == ["created_by_user_id"]:
            op.drop_constraint(foreign_key["name"], "tag", type_="foreignkey")
    for name in (
        "normalized_name",
        "is_active",
        "created_by_user_id",
        "created_at",
    ):
        if name in _columns("tag"):
            op.drop_column("tag", name)
    checks = {
        item["name"] for item in inspect(bind).get_check_constraints("content")
    }
    if "ck_content_workflow_state_values" in checks:
        op.execute(
            sa.text(
                "UPDATE content SET workflow_state = CASE "
                "WHEN workflow_state = '已制作' THEN '待排期' "
                "WHEN workflow_state = '未发布' THEN '待制作' "
                "ELSE workflow_state END"
            )
        )
        op.drop_constraint(
            "ck_content_workflow_state_values", "content", type_="check"
        )
    op.create_check_constraint(
        "ck_content_workflow_state_values",
        "content",
        "workflow_state IN ('待制作', '待排期', '已排期', '已发布')",
    )
