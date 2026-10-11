# SPDX-License-Identifier: Apache-2.0
"""Run database migrations before starting the API in the container."""

import os
import subprocess
import sys
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[1]
subprocess.run(
    [sys.executable, "-m", "alembic", "upgrade", "head"],
    check=True,
    cwd=PROJECT_ROOT,
)
if os.getenv("SEED_DATA", "false").strip().lower() == "true":
    subprocess.run(
        [sys.executable, "scripts/seed_data.py", "--force"],
        check=True,
        cwd=PROJECT_ROOT,
    )
os.execvpe(
    sys.executable,
    [
        sys.executable,
        "-m",
        "uvicorn",
        "backend.main:app",
        "--host",
        "0.0.0.0",
        "--port",
        "8000",
    ],
    os.environ,
)
