#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

if [[ -x ".venv/bin/python" ]]; then
  ".venv/bin/python" scripts/reset_database.py
  exit 0
fi

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  echo "未找到本地虚拟环境，使用 Docker Compose 删除 PostgreSQL 持久化卷。"
  read -r -p "这会清空 Docker 中的 arts_smart 数据库，继续请输入 RESET：" answer
  if [[ "$answer" != "RESET" ]]; then
    echo "已取消"
    exit 1
  fi
  # 只使用本地已有镜像，不拉取、不构建，也不需要外网。
  docker compose down -v --remove-orphans
  docker compose up -d --no-build
  echo "Docker PostgreSQL 数据卷已重建，应用容器已启动并会自动执行迁移。"
  exit 0
fi

echo "未找到 .venv/bin/python，也未检测到 Docker Compose。"
exit 1
