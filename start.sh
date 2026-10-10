#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
if [ ! -d .venv ]; then python3 -m venv .venv; fi
.venv/bin/python -m pip install -r requirements.txt
if [ ! -f .env ]; then cp .env.example .env; fi
mkdir -p data
.venv/bin/alembic upgrade head
exec .venv/bin/uvicorn backend.main:app --host 127.0.0.1 --port 8000
