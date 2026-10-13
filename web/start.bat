@echo off
rem ============================================================================
rem  艺数智策 · 前端一键启动（Windows 非 Docker 环境）
rem
rem  职责：检查环境、安装缺失依赖、启动 Vite 开发服务器、等待真正就绪后
rem        打开默认浏览器。日志留在当前窗口。
rem  后端地址：来自 web\.env。VITE_DEV_API_TARGET 是开发服务器 /api 的代理目标
rem            （当前默认 http://59.110.90.28:8080）；VITE_API_BASE_URL 留空表示
rem            浏览器只访问同源 /api，由开发服务器转发，不触发跨域。
rem  边界：不启动/停止 Go 后端，不初始化或迁移数据库，不依赖 Docker，
rem        不覆盖已有 .env，不删除任何数据。
rem  退出码：0 成功（开发服务器正常退出）；1 缺少 Node/npm；2 依赖安装失败；
rem          3 未找到 package.json；4 .env 缺失且无法创建；5 .env 配置错误；
rem          6 开发服务器启动失败。
rem ============================================================================
setlocal enabledelayedexpansion
chcp 65001 >nul
cd /d "%~dp0"
set "SCRIPT_DIR=%~dp0"
set "START_FAILED=0"

echo.
echo === 艺数智策 · 前端启动 ===
echo 工作目录：%SCRIPT_DIR%
echo.

rem --- 1. 基础文件检查 -------------------------------------------------------
if not exist "package.json" (
  echo [错误] 未找到 %SCRIPT_DIR%package.json，请在 web 目录中运行本脚本。
  endlocal
  exit /b 3
)

rem --- 2. Node.js 与 npm ------------------------------------------------------
where node >nul 2>nul
if errorlevel 1 (
  echo [错误] 未检测到 Node.js。请安装 Node.js 20 或更高版本，并确保 node 已加入 PATH。
  echo        安装后请重新打开命令行再运行本脚本。
  endlocal
  exit /b 1
)
where npm >nul 2>nul
if errorlevel 1 (
  echo [错误] 未检测到 npm。请重新安装 Node.js（含 npm），或检查 PATH 配置。
  endlocal
  exit /b 1
)
for /f "delims=" %%v in ('node --version 2^>nul') do set "NODE_VERSION=%%v"
for /f "delims=" %%v in ('npm --version 2^>nul') do set "NPM_VERSION=%%v"
echo [1/5] Node.js %NODE_VERSION% · npm %NPM_VERSION%

rem --- 3. 依赖检查（仅缺失时安装） -------------------------------------------
set "VITE_BIN=node_modules\.bin\vite.cmd"
if exist "%VITE_BIN%" goto :deps_ok
echo [2/5] 依赖缺失，开始安装（首次运行会稍慢）...
if exist "package-lock.json" (
  call npm ci
) else (
  call npm install
)
if errorlevel 1 (
  echo.
  echo [错误] 依赖安装失败。请检查网络或 npm registry 配置后重试。
  echo        企业网络可执行：npm config set registry ^<你的镜像地址^>
  endlocal
  exit /b 2
)
if not exist "%VITE_BIN%" (
  echo [错误] 安装完成但未找到 %VITE_BIN%，依赖可能不完整。请删除 node_modules 后重试。
  endlocal
  exit /b 2
)
goto :deps_done
:deps_ok
echo [2/5] 依赖已存在，跳过安装
:deps_done

rem --- 4. 环境变量检查 -------------------------------------------------------
if not exist ".env" (
  if exist ".env.example" (
    copy /y ".env.example" ".env" >nul
    echo [3/5] 已从 .env.example 创建 .env
  ) else (
    echo [错误] 缺少 .env 与 .env.example，无法确定后端 API 地址。
    endlocal
    exit /b 4
  )
) else (
  echo [3/5] 已存在 .env（不会覆盖）
)

set "API_BASE="
set "DEV_TARGET="
for /f "tokens=1,* delims==" %%a in ('type ".env"') do (
  set "KEY=%%a"
  if /i "!KEY: =!"=="VITE_API_BASE_URL" set "API_BASE=%%b"
  if /i "!KEY: =!"=="VITE_DEV_API_TARGET" set "DEV_TARGET=%%b"
)
if defined API_BASE (
  rem 去掉首尾空格
  for /f "tokens=* delims= " %%t in ("!API_BASE!") do set "API_BASE=%%t"
)
if defined DEV_TARGET (
  for /f "tokens=* delims= " %%t in ("!DEV_TARGET!") do set "DEV_TARGET=%%t"
)
if defined DEV_TARGET (
  echo        开发代理目标 VITE_DEV_API_TARGET=!DEV_TARGET!
  echo        即页面上的 /api 请求都会由本机开发服务器转发到这里。
) else (
  echo        开发代理目标 VITE_DEV_API_TARGET 未设置：默认代理到 http://localhost:8080
)
if defined API_BASE (
  echo        VITE_API_BASE_URL=!API_BASE!
  rem 浏览器无法解析容器内部服务名（例如 api:8080 / backend / db）。这类配置
  rem 会让页面所有接口请求失败，因此在启动前直接报错。
  set "CHECK_HOST=!API_BASE!"
  set "INTERNAL_HOST="
  if /i "!CHECK_HOST:~0,4!"=="api:" set "INTERNAL_HOST=1"
  if /i "!CHECK_HOST:~0,7!"=="backend" set "INTERNAL_HOST=1"
  if /i "!CHECK_HOST:~0,3!"=="db:" set "INTERNAL_HOST=1"
  if /i "!CHECK_HOST:~0,4!"=="web:" set "INTERNAL_HOST=1"
  if /i "!CHECK_HOST:~0,8!"=="postgres" set "INTERNAL_HOST=1"
  if defined INTERNAL_HOST (
    echo.
    echo [错误] VITE_API_BASE_URL 使用了 Docker 内部服务名：!API_BASE!
    echo        浏览器无法解析该主机名。请在 web\.env 中把它改为：
    echo          VITE_API_BASE_URL=
    echo        留空表示同源访问，开发服务器与生产 Nginx 都会代理 /api 到后端。
    endlocal
    exit /b 5
  )
  rem 非空表示浏览器直连后端：后端必须允许本页面来源跨域（CORS_ALLOWED_ORIGINS），
  rem 否则接口会返回 403 跨域来源未获允许。远端后端通常没有开放跨域，所以推荐留空。
  echo        [提示] 该值非空表示浏览器直连后端，需要后端 CORS_ALLOWED_ORIGINS
  echo               允许本页面来源（默认 http://localhost:5173）；
  echo               若接口返回 403 跨域来源未获允许，请把它留空并改用
  echo               VITE_DEV_API_TARGET，由开发服务器代理转发。
) else (
  echo        VITE_API_BASE_URL 为空：浏览器只访问同源 /api，由开发服务器代理转发，不跨域
)

rem --- 4b. 后端连通性提示（只提示，不阻塞启动） ------------------------------
rem 页面本身不依赖后端也能打开，但如果后端不可达，所有接口都会失败。这里先用一次
rem TCP 连接探测（最多 3 秒），把原因直接打印在启动日志里，便于排查。
if defined DEV_TARGET (
  set "PROBE_URL=!DEV_TARGET!"
) else (
  set "PROBE_URL=http://localhost:8080"
)
rem 裸 host:port（缺少 http://）由 vite.config.ts 规范化，这里补上以便解析。
if not "!PROBE_URL:~0,4!"=="http" set "PROBE_URL=http://!PROBE_URL!"
set "PROBE_PS=try{$u=[uri]'!PROBE_URL!';$c=New-Object Net.Sockets.TcpClient;$a=$c.BeginConnect($u.Host,$u.Port,$null,$null);if($a.AsyncWaitHandle.WaitOne(3000)){$c.EndConnect($a);exit 0}}catch{};exit 1"
powershell -NoProfile -ExecutionPolicy Bypass -Command "!PROBE_PS!" >nul 2>nul
if errorlevel 1 (
  echo [提示] 后端 !PROBE_URL! 当前无法连接。页面仍会打开，但接口请求会失败。
  echo        请确认后端已启动、本机网络/VPN 能访问该地址，或修改 web\.env 的
  echo        VITE_DEV_API_TARGET 指向可达的后端后重新运行本脚本。
) else (
  echo        后端连通性正常：!PROBE_URL!
)

rem --- 5. 选择可用端口 -------------------------------------------------------
set "PORT="
if not defined WEB_DEV_PORT set "WEB_DEV_PORT=5173"
set "TRY_PORT=%WEB_DEV_PORT%"
for /l %%i in (0,1,9) do (
  if not defined PORT (
    netstat -ano | findstr /r /c:":!TRY_PORT! .*LISTENING" >nul 2>nul
    if errorlevel 1 (
      set "PORT=!TRY_PORT!"
    ) else (
      if %%i==0 echo [提示] 端口 !TRY_PORT! 已被占用，自动改用下一个可用端口。
      set /a "TRY_PORT=!TRY_PORT!+1"
    )
  )
)
if not defined PORT (
  echo [错误] 从 %WEB_DEV_PORT% 开始的 10 个端口都被占用。请关闭占用进程，
  echo        或先设置 WEB_DEV_PORT=其他端口 再运行本脚本。
  endlocal
  exit /b 6
)
set "APP_URL=http://localhost:%PORT%/"
rem 探测用 IPv4 回环：开发服务器绑定 0.0.0.0（仅 IPv4），而 Windows 上 localhost 可能先
rem 解析到 ::1，用 localhost 探测会白等一轮。浏览器仍然打开 http://localhost:端口/。
set "POLL_URL=http://127.0.0.1:%PORT%/"
echo [4/5] 使用端口 %PORT%

rem --- 6. 启动前先把"就绪检测"放到后台窗口 -----------------------------------
rem 检测到 HTTP 服务真正可用后才打开默认浏览器，避免出现空白页。
set "WAIT_PS=$u='%APP_URL%';$p='%POLL_URL%';for($i=0;$i -lt 90;$i++){try{$r=Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 -Uri $p;if($r.StatusCode -ge 200){Start-Process $u;exit 0}}catch{};Start-Sleep -Seconds 1};exit 1"
start "ArtSmart 前端就绪检测" /min powershell -NoProfile -ExecutionPolicy Bypass -Command "!WAIT_PS!"

rem --- 7. 在当前窗口启动开发服务器（日志可见） -------------------------------
echo [5/5] 启动开发服务器：%APP_URL%
echo        就绪后会自动打开浏览器；按 Ctrl+C 可停止服务。
echo.
call npm run dev -- --port %PORT% --strictPort
set "DEV_EXIT=!errorlevel!"
if not "!DEV_EXIT!"=="0" (
  set "START_FAILED=1"
  echo.
  echo [错误] 开发服务器启动失败或异常退出（退出码 !DEV_EXIT!）。
  echo        常见原因：端口 %PORT% 在启动瞬间被占用、依赖损坏、Node 版本过低。
  echo        可尝试：设置 WEB_DEV_PORT 换端口，或删除 node_modules 后重新运行本脚本。
)

if "!START_FAILED!"=="1" (
  endlocal
  exit /b 6
)
echo.
echo 开发服务器已停止。
endlocal
exit /b 0
