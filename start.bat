@echo off
rem ============================================================================
rem  艺数智策 · 一键启动前端（Windows，不需要 Docker）
rem
rem  作用：启动前端开发服务器，就绪后用默认浏览器打开页面；日志留在当前窗口。
rem        后端地址来自 web\.env：VITE_DEV_API_TARGET（默认 http://59.110.90.28:8080）
rem        是开发服务器 /api 的代理目标；浏览器只访问同源 /api，不涉及跨域。
rem  实现：直接调用 web\start.bat —— 环境检查、按需安装依赖、启动、就绪检测只有一份。
rem  不涉及：Docker、数据库、Go 后端进程。需要整套本机服务时改用 start-docker.bat。
rem  退出码（与 web\start.bat 一致）：1 缺少 Node/npm；2 依赖安装失败；3 缺少 package.json；
rem          4 缺少 .env 且无法创建；5 .env 配置错误；6 开发服务器启动失败。
rem ============================================================================
setlocal
cd /d "%~dp0"

if not exist "web\start.bat" (
  echo [错误] 未找到 web\start.bat，无法启动前端。
  echo        请确认在完整的项目目录中运行本脚本（前端代码位于 web 子目录）。
  endlocal
  exit /b 3
)

where node >nul 2>nul
if errorlevel 1 (
  echo [错误] 未检测到 Node.js。请安装 Node.js 20 或更高版本，并确保 node 已加入 PATH。
  echo        安装后请重新打开命令行再运行本脚本。
  endlocal
  exit /b 1
)

echo.
echo === 艺数智策 · 一键启动前端（不使用 Docker）===
echo 后端地址来自 web\.env，由开发服务器代理 /api；就绪后自动打开浏览器。
echo.

call "%~dp0web\start.bat"
set "FRONTEND_EXIT=%ERRORLEVEL%"
if not "%FRONTEND_EXIT%"=="0" (
  echo.
  echo [错误] 前端启动流程未正常完成（退出码 %FRONTEND_EXIT%），原因见上面的输出。
)
endlocal & exit /b %FRONTEND_EXIT%
