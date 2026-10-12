@echo off
setlocal
cd /d "%~dp0"

where docker >nul 2>nul
if errorlevel 1 (
  echo Docker Desktop is required. Install or start Docker Desktop first.
  exit /b 3
)

docker compose version >nul 2>nul
if errorlevel 1 (
  echo Docker Compose v2 is required. Update Docker Desktop and try again.
  exit /b 3
)

if not exist ".env" (
  copy ".env.example" ".env" >nul
  echo Created .env from .env.example.
  echo Set POSTGRES_ADMIN_PASSWORD, APP_DB_PASSWORD, and APP_SECRET in .env.
  exit /b 2
)

docker compose config --quiet
if errorlevel 1 (
  echo Compose configuration is incomplete. Check required values in .env.
  exit /b 4
)

docker compose up --build --detach --wait
set "START_RESULT=%ERRORLEVEL%"
if not "%START_RESULT%"=="0" (
  echo Startup failed. Recent service logs follow.
  docker compose ps
  docker compose logs --tail=80 db api web
  exit /b %START_RESULT%
)

echo ArtSmart is ready at http://localhost/ and http://127.0.0.1/ by default.
echo API health is available at http://localhost/api/health/ready.
endlocal
