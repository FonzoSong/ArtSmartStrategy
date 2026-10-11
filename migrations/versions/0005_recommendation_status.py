# SPDX-License-Identifier: Apache-2.0
"""Persist recommendation analysis and AI fallback status.

Revision ID: 0005_recommendation_status
Revises: 0004_performance_workflow
"""

import sqlalchemy as sa
from alembic import op
from sqlalchemy import inspect

revision = "0005_recommendation_status"
down_revision = "0004_performance_workflow"
branch_labels = None
depends_on = None


def upgrade() -> None:
    """Add nullable-compatible analysis status columns."""
    columns = {
        item["name"]
        for item in inspect(op.get_bind()).get_columns("recommendation")
    }
    if "analysis_status" not in columns:
        op.add_column(
            "recommendation",
            sa.Column(
                "analysis_status",
                sa.String(40),
                nullable=False,
                server_default="rules_only",
            ),
        )
    if "fallback_reason" not in columns:
        op.add_column(
            "recommendation",
            sa.Column(
                "fallback_reason", sa.Text(), nullable=False, server_default=""
            ),
        )


def downgrade() -> None:
    """Drop recommendation analysis status columns."""
    columns = {
        item["name"]
        for item in inspect(op.get_bind()).get_columns("recommendation")
    }
    if "fallback_reason" in columns:
        op.drop_column("recommendation", "fallback_reason")
    if "analysis_status" in columns:
        op.drop_column("recommendation", "analysis_status")
