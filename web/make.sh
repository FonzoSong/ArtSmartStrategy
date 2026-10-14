#!/usr/bin/env bash
# ============================================================================
# 艺数智策 · web 构建脚本
#
#  用法：在 web/ 目录下执行  bash make.sh
#  产出：web/out/ —— 可独立拷贝到任意 Windows 机器的自包含前端
#        （编译后的 JS、静态资源、server.js、start.bat、README.txt）
#
#  设计要点：
#    · 复用项目已有构建链 Vite（web/vite.config.ts），不引入第二套打包器；
#    · 通过命令行参数把 outDir 指到 web/out，源码目录不被写入；
#    · server.js / start.bat / README.txt 来自 web/runtime/ 模板，批处理统一转成
#      CRLF（Windows cmd.exe 对 LF 换行的多行 if 块和 goto 很敏感）；
#    · 幂等：每次运行先清空 out/ 再重建；任一步失败立即退出并打印原因；
#    · 只用 POSIX 工具（不用 sed -i、rsync、GNU 专属选项），Git Bash 可直接运行。
#
#  不使用：Docker、WSL、任何需要联网安装的新依赖。
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="$SCRIPT_DIR/out"
RUNTIME_DIR="$SCRIPT_DIR/runtime"

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

step 1/7 "检查环境"
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

step 2/7 "清理并创建 out/"
rm -rf -- "$OUT_DIR"
mkdir -p -- "$OUT_DIR"
printf '  已重建 %s\n' "$OUT_DIR"

cd "$SCRIPT_DIR"

step 3/7 "TypeScript 类型检查（tsc --noEmit，与 web/Dockerfile 的 npm run typecheck 一致）"
node node_modules/typescript/bin/tsc --noEmit || die "TypeScript 类型检查未通过，请先修复上面的报错。"

step 4/7 "编译 TypeScript 并打包到 out/（Vite，复用 web/vite.config.ts）"
node node_modules/vite/bin/vite.js build --outDir out --emptyOutDir --logLevel info ||
  die "Vite 构建失败，请检查上面的报错。"

step 5/7 "校验编译产物并复制静态资源"
[ -f "$OUT_DIR/index.html" ] || die "构建产物缺少 out/index.html。"
if ! ls "$OUT_DIR"/assets/*.js >/dev/null 2>&1; then
  die "构建产物缺少 out/assets/*.js（编译后的 JS 代码）。"
fi
if [ -d "$SCRIPT_DIR/public" ]; then
  cp -R "$SCRIPT_DIR/public/." "$OUT_DIR/" || die "复制 public/ 静态资源失败。"
  printf '  已复制 public/ 下的静态资源（保持相对路径不变）\n'
fi

step 6/7 "写入运行期文件（server.js / start.bat / README.txt / api-target.txt / package.json）"
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

step 7/7 "校验产出并汇总"
for required in index.html start.bat server.js README.txt package.json api-target.txt; do
  [ -s "$OUT_DIR/$required" ] || die "产出不完整：缺少 out/$required。"
done

printf '\n构建完成：%s\n' "$OUT_DIR"
printf '文件清单：\n'
(
  cd "$OUT_DIR"
  find . -type f | sed 's|^\./||' | sort | while IFS= read -r file; do
    printf '  %s\n' "$file"
  done
)
printf '\n下一步：双击 out\\start.bat（或在 out/ 目录下执行 node server.js）即可启动前端服务。\n'
