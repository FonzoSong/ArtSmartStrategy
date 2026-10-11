# SPDX-License-Identifier: Apache-2.0
"""Create the initial application schema.

Revision ID: 0001_initial
Revises:
"""

from alembic import op
from backend import models  # noqa: F401
from backend.database import Base

revision = "0001_initial"
down_revision = None
branch_labels = None
depends_on = None


def upgrade():
    Base.metadata.create_all(bind=op.get_bind())


def downgrade():
    Base.metadata.drop_all(bind=op.get_bind())
