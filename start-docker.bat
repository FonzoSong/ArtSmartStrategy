@echo off
rem ============================================================================
rem  艺数智策 · 完整技术栈启动（Docker Compose，可选，需要 Docker Desktop）
rem
rem  用途：只在本机需要整套服务（PostgreSQL + Go API + Nginx 托管的前端）时使用。
rem        启动完成后访问 http://localhost/ 或 http://127.0.0.1/，API 走同源 /api。
rem  不需要 Docker 的日常开发：双击项目根目录的 start.bat（等同于 web\start.bat），
rem        它只启动前端开发服务器并自动打开浏览器，后端地址由 web\.env 决定
rem        （默认远端 http://59.110.90.28:8080）。
rem  退出码：3 缺少 Docker Desktop 或 Compose v2；2 缺少 .env；4 Compose 配置不完整；
rem          其他为 docker compose 自身的退出码。
rem ============================================================================
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
