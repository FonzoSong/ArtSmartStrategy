@echo off
setlocal
cd /d "%~dp0"

where py >nul 2>nul
if errorlevel 1 (
  echo Python Launcher was not found. Install Python 3.11 or newer and enable the py launcher.
  goto :failed
)

if not exist ".venv\Scripts\python.exe" (
  py -3 -m venv .venv
  if errorlevel 1 goto :failed
)

if not exist ".env" (
  copy ".env.example" ".env" >nul
  echo Created .env from .env.example.
  echo Edit DATABASE_URL and APP_SECRET before continuing, then run start.bat again.
  notepad .env
  goto :finished
)

call .venv\Scripts\python.exe -m pip install --disable-pip-version-check -r requirements.txt
if errorlevel 1 goto :failed

call .venv\Scripts\python.exe scripts\ensure_database.py
if errorlevel 1 goto :failed

call .venv\Scripts\python.exe -m alembic upgrade head
if errorlevel 1 goto :failed

echo Starting Artsmart at http://127.0.0.1:8000
call .venv\Scripts\python.exe -m uvicorn backend.main:app --host 127.0.0.1 --port 8000
goto :finished

:failed
echo.
echo Startup failed. Check the message above and the Windows setup section in README.md.
pause

:finished
endlocal
