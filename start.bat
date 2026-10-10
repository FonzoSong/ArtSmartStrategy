@echo off
cd /d %~dp0
if not exist .venv python -m venv .venv
call .venv\Scripts\python -m pip install -r requirements.txt
if not exist .env copy .env.example .env
if not exist data mkdir data
call .venv\Scripts\alembic upgrade head
call .venv\Scripts\python -m uvicorn backend.main:app --host 127.0.0.1 --port 8000
