#!/usr/bin/env bash
# ============================================================================
# 艺数智策 · web 构建脚本
#
#  用法：在 web/ 目录下执行  bash make.sh
#  产出：web/out/ —— 可独立拷贝到任意 Windows 机器的自包含前端
#        （编译后的 JS、静态资源、server.js、start.bat、README.txt，
#          以及打包进来的 Windows 版 node.exe）
#
#  设计要点：
#    · 复用项目已有构建链 Vite（web/vite.config.ts），不引入第二套打包器；
#    · 通过命令行参数把 outDir 指到 web/out，源码目录不被写入；
#    · 默认把官方 Windows 版 node.exe 放进 out/node/：目标机器不需要安装
#      Node.js、不需要联网。二进制取自 https://nodejs.org/dist 官方发布，
#      下载后按官方 SHASUMS256.txt 校验 SHA-256，并缓存到 web/.cache/node/
#      复用，不会每次重复下载；
#    · server.js / start.bat / README.txt 来自 web/runtime/ 模板，批处理统一转成
#      CRLF（Windows cmd.exe 对 LF 换行的多行 if 块和 goto 很敏感）；
#    · 幂等：每次运行先清空 out/ 再重建；任一步失败立即退出并打印原因；
#    · 只用 POSIX 工具（不用 sed -i、rsync、GNU 专属选项），Git Bash 可直接运行。
#
#  不使用：Docker、WSL、npm 依赖。唯一可能联网的动作是下载官方 node.exe，
#          可用 --no-bundle-node 关掉（目标机器自带 Node.js 即可）。
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="$SCRIPT_DIR/out"
RUNTIME_DIR="$SCRIPT_DIR/runtime"

# —— 命令行参数（默认行为：打包 node.exe）-----------------------------------
BUNDLE_NODE_MODE="auto"          # auto | yes | no
NODE_BUNDLE_VERSION="v24.21.0"   # 写 latest-lts 则取官方当前 LTS
NODE_BUNDLE_VERSION_FALLBACK="v24.21.0"
NODE_BUNDLE_ARCH="x64"           # x64 | arm64
while [ "$#" -gt 0 ]; do
  case "$1" in
    --bundle-node) BUNDLE_NODE_MODE="yes" ;;
    --no-bundle-node) BUNDLE_NODE_MODE="no" ;;
    --node-version)
      shift
      [ "$#" -gt 0 ] || { echo "[错误] --node-version 需要版本号，例如 --node-version v24.21.0" >&2; exit 2; }
      NODE_BUNDLE_VERSION="$1"
      ;;
    --node-version=*) NODE_BUNDLE_VERSION="${1#--node-version=}" ;;
    --node-arch)
      shift
      [ "$#" -gt 0 ] || { echo "[错误] --node-arch 需要 x64 或 arm64" >&2; exit 2; }
      NODE_BUNDLE_ARCH="$1"
      ;;
    --node-arch=*) NODE_BUNDLE_ARCH="${1#--node-arch=}" ;;
    -h | --help)
      cat <<'USAGE'
用法：bash make.sh [选项]

  --bundle-node        把 Windows 版 node.exe 打包进 out/node/（默认行为）
  --no-bundle-node     不打包 node.exe，目标机器自行安装 Node.js 20+
  --node-version VER   指定 Node 版本，默认 v24.21.0；写 latest-lts 取当前 LTS
  --node-arch ARCH     目标架构，默认 x64；Windows ARM64 用 arm64

说明：node.exe 取自 https://nodejs.org/dist 官方发布，下载后按官方
      SHASUMS256.txt 校验 SHA-256，并缓存到 web/.cache/node/ 复用。
      打包后目标机器零安装、零联网即可双击 start.bat 运行。
USAGE
      exit 0
      ;;
    *) echo "[错误] 未知参数：$1（可用 --bundle-node / --no-bundle-node / --node-version / --node-arch / --help）" >&2; exit 2 ;;
  esac
  shift
done

case "$NODE_BUNDLE_ARCH" in
  x64 | arm64) ;;
  *) echo "[错误] --node-arch 只支持 x64 或 arm64（收到：$NODE_BUNDLE_ARCH）" >&2; exit 2 ;;
esac

step() { printf '\n[%s] %s\n' "$1" "$2"; }
die() { printf '\n[错误] %s\n' "$1" >&2; exit 1; }

# 读取 .env 风格的配置值：忽略注释行、去掉首尾空白与成对引号、按第一个 = 切分。
# 用 awk 而不是 sed -i，避免 Git Bash / BusyBox 上的不兼容。
read_env_value() {
  local file="$1" key="$2"
  [ -f "$file" ] || return 0
  awk -v key="$key" '
    {
      line = $0
      sub(/\r$/, "", line)
      sub(/^[ \t]+/, "", line)
      if (line == "" || substr(line, 1, 1) == "#") next
      split(line, parts, "=")
      name = parts[1]
      gsub(/[ \t]+$/, "", name)
      if (name != key) next
      value = substr(line, length(name) + 2)
      gsub(/^[ \t]+/, "", value)
      gsub(/[ \t]+$/, "", value)
      first = substr(value, 1, 1)
      last = substr(value, length(value), 1)
      if ((first == "\"" && last == "\"") || (first == "\047" && last == "\047")) {
        value = substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "$file"
}

# 复制模板并把换行统一成 CRLF：Windows 优先的文件由生成脚本负责换行差异。
copy_crlf() {
  local src="$1" dst="$2"
  awk 'BEGIN { ORS = "\r\n" } { sub(/\r$/, ""); print }' "$src" >"$dst" || die "写入失败：$dst"
  [ -s "$dst" ] || die "生成的文件为空：$dst"
}

# —— 打包 Windows 版 node.exe 用到的工具函数 ---------------------------------
NODE_CACHE_DIR="$SCRIPT_DIR/.cache/node"

# 下载到文件：优先 curl（Git Bash 自带 curl.exe），其次 wget。
download_file() {
  local url="$1" dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 --connect-timeout 15 --max-time 900 -o "$dest" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -q --timeout=900 -O "$dest" "$url"
  else
    return 1
  fi
}

# 计算 SHA-256：sha256sum（Linux / Git Bash）→ shasum（macOS）→ openssl。
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  elif command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$1" | awk '{print $NF}'
  fi
}

# --node-version latest-lts：用 Node 读官方 index.json 取当前 LTS 版本号。
resolve_node_version() {
  local resolved=""
  resolved="$(node -e 'fetch("https://nodejs.org/dist/index.json").then(r=>r.json()).then(list=>{const lts=list.find(v=>v.lts);process.stdout.write(lts?lts.version:"")}).catch(()=>process.stdout.write(""))' 2>/dev/null || true)"
  if [ -n "$resolved" ]; then
    printf '%s' "$resolved"
  else
    printf '%s' "$NODE_BUNDLE_VERSION_FALLBACK"
  fi
}

# 把官方 node.exe 放进 out/node/。成功返回 0；失败返回 1（由调用方决定是否致命）。
# 顺序：Windows 上构建直接复用本机 node.exe → 本地缓存 → 下载 + SHA-256 校验。
bundle_node_exe() {
  case "$(uname -s 2>/dev/null || echo unknown)" in
    MINGW* | MSYS* | CYGWIN*)
      local local_node
      local_node="$(command -v node 2>/dev/null || true)"
      if [ -n "$local_node" ] && [ -f "$local_node" ]; then
        mkdir -p "$OUT_DIR/node"
        cp "$local_node" "$OUT_DIR/node/node.exe" || return 1
        printf '  复用本机 node.exe：%s（%s，无需下载）\n' "$local_node" "$(node --version 2>/dev/null)"
        return 0
      fi
      ;;
  esac

  if [ "$NODE_BUNDLE_VERSION" = "latest-lts" ]; then
    NODE_BUNDLE_VERSION="$(resolve_node_version)"
    printf '  latest-lts → %s\n' "$NODE_BUNDLE_VERSION"
  fi

  local url_base="https://nodejs.org/dist/${NODE_BUNDLE_VERSION}"
  local node_url="${url_base}/win-${NODE_BUNDLE_ARCH}/node.exe"
  local shasums_url="${url_base}/SHASUMS256.txt"
  local cached_exe="${NODE_CACHE_DIR}/node-${NODE_BUNDLE_VERSION}-win-${NODE_BUNDLE_ARCH}.exe"
  local shasums_file="${NODE_CACHE_DIR}/SHASUMS256-${NODE_BUNDLE_VERSION}.txt"

  mkdir -p "$NODE_CACHE_DIR"
  if [ -s "$cached_exe" ]; then
    printf '  命中本地缓存：%s\n' "$cached_exe"
  else
    printf '  下载 %s\n' "$node_url"
    printf '  （官方 node.exe 约 80 MB，只下载一次；缓存目录 %s）\n' "$NODE_CACHE_DIR"
    if ! download_file "$node_url" "${cached_exe}.part"; then
      rm -f "${cached_exe}.part"
      printf '  下载失败：%s\n' "$node_url"
      return 1
    fi
    mv "${cached_exe}.part" "$cached_exe"
  fi

  # 校验完整性：官方 SHASUMS256.txt 里 win-<arch>/node.exe 那一行。
  local expected="" actual=""
  if download_file "$shasums_url" "$shasums_file"; then
    expected="$(awk -v target="win-${NODE_BUNDLE_ARCH}/node.exe" '$2 == target { print $1; exit }' "$shasums_file")"
  fi
  actual="$(sha256_of "$cached_exe")"
  if [ -n "$expected" ] && [ -n "$actual" ]; then
    if [ "$expected" != "$actual" ]; then
      rm -f "$cached_exe"
      printf '  SHA-256 校验失败（期望 %s，实际 %s），已删除损坏的缓存文件\n' "$expected" "$actual"
      return 1
    fi
    printf '  SHA-256 校验通过：%s…\n' "$(printf '%s' "$actual" | cut -c1-16)"
  else
    printf '  提示：未取到官方校验值，跳过 SHA-256 校验\n'
  fi

  mkdir -p "$OUT_DIR/node"
  cp "$cached_exe" "$OUT_DIR/node/node.exe" || return 1
  printf '  out/node/node.exe ← %s win-%s（%s）\n' \
    "$NODE_BUNDLE_VERSION" "$NODE_BUNDLE_ARCH" "$(du -h "$OUT_DIR/node/node.exe" | awk '{print $1}')"
  return 0
}

step 1/8 "检查环境"
case "$OUT_DIR" in
  "" | "/" | "$SCRIPT_DIR" | "$SCRIPT_DIR/") die "输出目录异常，已中止：$OUT_DIR" ;;
esac
[ -f "$SCRIPT_DIR/package.json" ] || die "未找到 package.json：请在 web/ 目录下运行本脚本。"
[ -d "$RUNTIME_DIR" ] || die "未找到模板目录 $RUNTIME_DIR（内含 server.js / start.bat.template）。"
for template in server.js start.bat.template README.txt.template; do
  [ -f "$RUNTIME_DIR/$template" ] || die "缺少模板文件：$RUNTIME_DIR/$template"
done
command -v node >/dev/null 2>&1 || die "未检测到 Node.js：请安装 Node.js 20 或更高版本后重试。"
[ -f "$SCRIPT_DIR/node_modules/vite/bin/vite.js" ] ||
  die "缺少构建依赖 vite：请先在 web/ 目录执行 npm install（一次性，需要网络）后再运行 make.sh。"
[ -f "$SCRIPT_DIR/node_modules/typescript/bin/tsc" ] ||
  die "缺少构建依赖 typescript：请先在 web/ 目录执行 npm install（一次性，需要网络）后再运行 make.sh。"
printf '  Node.js %s · 源码目录 %s\n' "$(node --version)" "$SCRIPT_DIR"

step 2/8 "清理并创建 out/"
rm -rf -- "$OUT_DIR"
mkdir -p -- "$OUT_DIR"
printf '  已重建 %s\n' "$OUT_DIR"

cd "$SCRIPT_DIR"

step 3/8 "TypeScript 类型检查（tsc --noEmit，与 web/Dockerfile 的 npm run typecheck 一致）"
node node_modules/typescript/bin/tsc --noEmit || die "TypeScript 类型检查未通过，请先修复上面的报错。"

step 4/8 "编译 TypeScript 并打包到 out/（Vite，复用 web/vite.config.ts）"
node node_modules/vite/bin/vite.js build --outDir out --emptyOutDir --logLevel info ||
  die "Vite 构建失败，请检查上面的报错。"

step 5/8 "校验编译产物并复制静态资源"
[ -f "$OUT_DIR/index.html" ] || die "构建产物缺少 out/index.html。"
if ! ls "$OUT_DIR"/assets/*.js >/dev/null 2>&1; then
  die "构建产物缺少 out/assets/*.js（编译后的 JS 代码）。"
fi
if [ -d "$SCRIPT_DIR/public" ]; then
  cp -R "$SCRIPT_DIR/public/." "$OUT_DIR/" || die "复制 public/ 静态资源失败。"
  printf '  已复制 public/ 下的静态资源（保持相对路径不变）\n'
fi

step 6/8 "打包 Windows 版 node.exe（目标机器无需安装 Node.js）"
BUNDLED_NODE=""
if [ "$BUNDLE_NODE_MODE" = "no" ]; then
  printf '  已按 --no-bundle-node 跳过；目标机器需要自带 Node.js 20+\n'
elif bundle_node_exe; then
  BUNDLED_NODE="node/node.exe"
else
  if [ "$BUNDLE_NODE_MODE" = "yes" ]; then
    die "未能准备 Windows 版 node.exe：请检查网络，或把官方 node.exe 预先放到 $NODE_CACHE_DIR。"
  fi
  printf '  未打包 node.exe：目标机器需自带 Node.js 20+，start.bat 会自动回退到系统 Node。\n'
fi

step 7/8 "写入运行期文件（server.js / start.bat / README.txt / api-target.txt / package.json）"
copy_crlf "$RUNTIME_DIR/server.js" "$OUT_DIR/server.js"
copy_crlf "$RUNTIME_DIR/start.bat.template" "$OUT_DIR/start.bat"
copy_crlf "$RUNTIME_DIR/README.txt.template" "$OUT_DIR/README.txt"
printf '%s\r\n' \
  '{' \
  '  "name": "artsmart-web-out",' \
  '  "private": true,' \
  '  "description": "Self-contained static build output. Pins CommonJS so server.js stays require()-based even when out/ sits inside the repo (web/package.json declares type: module). Do not delete.",' \
  '  "type": "commonjs"' \
  '}' >"$OUT_DIR/package.json"

# /api 的代理目标：命令行环境变量 > web/.env > web/.env.example（后端在本机时通常
# 可直接删掉 out/api-target.txt 或把它改成 http://localhost:8080）。
API_TARGET="${ARTSMART_API_TARGET:-}"
if [ -z "$API_TARGET" ]; then
  API_TARGET="$(read_env_value "$SCRIPT_DIR/.env" VITE_DEV_API_TARGET)"
fi
if [ -z "$API_TARGET" ]; then
  API_TARGET="$(read_env_value "$SCRIPT_DIR/.env.example" VITE_DEV_API_TARGET)"
fi
if [ -n "$API_TARGET" ]; then
  printf '%s\r\n' "$API_TARGET" >"$OUT_DIR/api-target.txt"
  printf '  /api 代理目标：%s（写在 out/api-target.txt，可自行修改）\n' "$API_TARGET"
else
  printf '# 未配置后端地址：/api 请求会返回 502，页面会提示"未配置后端 API"。\r\n' >"$OUT_DIR/api-target.txt"
  printf '# 需要联调后端时，把下面一行改成后端地址（去掉行首的 #）后重新运行 start.bat。\r\n' >>"$OUT_DIR/api-target.txt"
  printf '# http://localhost:8080\r\n' >>"$OUT_DIR/api-target.txt"
  printf '  未配置 /api 代理目标：纯静态预览模式\n'
fi

step 8/8 "校验产出并汇总"
for required in index.html start.bat server.js README.txt package.json api-target.txt; do
  [ -s "$OUT_DIR/$required" ] || die "产出不完整：缺少 out/$required。"
done
if [ -n "$BUNDLED_NODE" ]; then
  [ -s "$OUT_DIR/$BUNDLED_NODE" ] || die "产出不完整：缺少 out/$BUNDLED_NODE。"
fi

printf '\n构建完成：%s\n' "$OUT_DIR"
printf '文件清单：\n'
(
  cd "$OUT_DIR"
  find . -type f | sed 's|^\./||' | sort | while IFS= read -r file; do
    printf '  %s\n' "$file"
  done
)
if [ -n "$BUNDLED_NODE" ]; then
  printf '  内置运行时：out/node/node.exe（目标 Windows 机器不需要安装 Node.js）\n'
else
  printf '  运行方式：server.js + 系统 Node.js（本次未打包 node.exe）\n'
fi
printf '  产物体积：%s\n' "$(du -sh "$OUT_DIR" | awk '{print $1}')"
printf '\n下一步：把 out/ 整个拷到目标机器，双击 out\\start.bat 即可启动前端服务。\n'
