@echo off
setlocal
cd /d "%~dp0"
if not exist .env (
  copy .env.example .env >nul
  echo Created .env from .env.example.
  echo Edit .env and set DATABASE_URL and APP_SECRET before starting.
  exit /b 2
)
where docker >nul 2>nul
if errorlevel 1 (
  echo Docker Desktop is required. Install or start Docker Desktop first.
  exit /b 3
)
docker compose up --build -d
if errorlevel 1 exit /b %errorlevel%
echo ArtSmart is starting. Frontend: http://localhost:8081  API: http://localhost:8080/api/health/ready
endlocal
