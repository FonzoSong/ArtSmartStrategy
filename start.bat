@echo off
setlocal
cd /d "%~dp0"

set "SEED_FLAG="
set "FORCE_FLAG="
if /I "%~1"=="--seed" set "SEED_FLAG=1"
if /I "%~1"=="seed" set "SEED_FLAG=1"
if /I "%~1"=="--force" (
  set "SEED_FLAG=1"
  set "FORCE_FLAG=1"
)
if /I "%~2"=="--force" set "FORCE_FLAG=1"
if not "%~1"=="" if not defined SEED_FLAG (
  echo Usage: start.bat [--seed^|seed] [--force]
  goto :failed
)

where py >nul 2>nul
if errorlevel 1 (
  echo Python Launcher was not found. Install Python 3.10 or newer and enable the py launcher.
  goto :failed
)

if not exist ".venv\Scripts\python.exe" (
  py -3 -m venv .venv
  if errorlevel 1 goto :failed
)

if not exist ".env" (
  copy ".env.example" ".env" >nul
  echo Created .env from .env.example.
  echo Edit DATABASE_URL if needed. APP_SECRET is generated automatically on launch.
  notepad .env
)

call .venv\Scripts\python.exe -m pip install --disable-pip-version-check -r requirements.txt
if errorlevel 1 goto :failed

call .venv\Scripts\python.exe -c "import backend.settings"
if errorlevel 1 goto :failed

set "SEED_DATA=false"
call .venv\Scripts\python.exe -m backend.container_entrypoint migrate
if errorlevel 1 goto :failed

if defined SEED_FLAG if defined FORCE_FLAG (
  call .venv\Scripts\python.exe scripts\seed_data.py --force
  if errorlevel 1 goto :failed
)
if defined SEED_FLAG if not defined FORCE_FLAG (
  call .venv\Scripts\python.exe scripts\seed_data.py
  if errorlevel 1 goto :failed
)

echo Starting Artsmart at http://127.0.0.1:8000
call .venv\Scripts\python.exe -m backend.container_entrypoint serve
goto :finished

:failed
echo.
echo Startup failed. Check the message above and the Windows setup section in README.md.
pause

:finished
endlocal
