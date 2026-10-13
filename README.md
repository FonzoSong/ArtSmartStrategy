# 艺数智策

艺术团队内容运营系统，采用 Go REST API、独立 TypeScript/Vite 前端和
Docker Compose PostgreSQL。浏览器仅通过同源 `/api` 访问后端；数据库只在内部网络
接受 API 和运维连接。

## 架构

- `cmd/api`：Go 服务入口、连接池、有限重试、Schema 迁移与优雅关闭。
- `internal/httpapi`：REST API、认证、权限、CORS 和统一错误响应。
- `internal/repository`：PostgreSQL 查询、团队隔离和事务。
- `internal/migrate/sql`：版本化 PostgreSQL schema 迁移。
- `web`：独立 Vite/TypeScript 应用；生产由 Nginx 代理 `/api/`。页面包含首页、运营总览、
  内容记录、正式演出（预热视频聚合与整场排期）、效果分析、排期建议与排期日历、模型设置、
  团队管理、内容规则集与 Tag 库；开发说明见 [web/README.md](web/README.md)。
- `db/init`、`db/sql`：首次建库 schema 和独立的可选示例 SQL。

业务数据持久化在 PostgreSQL named volume `arts-smart-postgres-data`。服务不能连接数据库
时会有限重试后失败，不会改用本地文件。项目不再包含本地业务数据库或运行时文件导入 API。

## 配置和启动

安装 Docker Desktop（含 Compose v2），将 `.env.example` 复制为 `.env`，配置
`POSTGRES_ADMIN_PASSWORD`、`APP_DB_PASSWORD` 和至少 32 字符的 `APP_SECRET`。可选配置
`AI_API_KEY`；密钥只从后端环境注入，不会回显给浏览器。`TZ`（默认 `Asia/Shanghai`）决定
正式演出的“已开演”判定与日期渲染，团队不在东八区时请一并调整。

```text
docker compose config -q
docker compose up --build --detach --wait
docker compose ps
docker compose logs -f api
```

Windows 可运行 `start-docker.bat`，其执行相同的 Docker Compose 启动流程。Web 监听本机 80
端口，可通过 `http://localhost/` 或 `http://127.0.0.1/` 访问；API 经同源 `/api` 代理访问。PostgreSQL 使用 `db` 服务名，
仅加入内部 `db-net`，没有宿主机端口映射。API 运行账号是非超级用户。只做前端时用根目录的
`start.bat`，它不需要 Docker（见「本机开发」）。

`LOAD_EXAMPLE_DATA=true` 仅控制数据库第一次初始化时是否执行
`db/sql/02_seed_data.sql`。新环境默认导入虚构样例；现有数据库卷不会再次执行初始化脚本。
该 SQL 可幂等重跑，但不会自动随容器重启执行。不要运行 `docker compose down -v`，除非
明确要删除数据库卷及其中全部数据。

## 三台服务器独立部署

`deploy/db`、`deploy/api`、`deploy/web` 各自提供 Compose 文件和独立 `.env.example`。将项目
仓库部署到对应服务器，把对应示例复制为该目录的 `.env` 并填写配置。数据库
地址使用仅 API 服务器可访问的私有 IP 或 VPN 地址；防火墙限制 5432 仅允许 API 主机，API
端口 8080 仅允许 Web 主机。不要将数据库端口暴露到公网。Web Nginx 通过配置的
`API_UPSTREAM` 反向代理 API，浏览器仍访问 Web 的同源 `/api`。

分别在三个服务器执行对应命令（先将 `.env.example` 复制为 `.env`）：

```text
docker compose --env-file deploy/db/.env -f deploy/db/compose.yaml up --build --detach --wait
docker compose --env-file deploy/api/.env -f deploy/api/compose.yaml up --build --detach --wait
docker compose --env-file deploy/web/.env -f deploy/web/compose.yaml up --build --detach --wait
```

前端服务器需将 `WEB_BIND_ADDRESS` 设为可供用户或 TLS 反向代理访问的接口地址；本机测试
设为 `127.0.0.1`。API 服务器 `CORS_ALLOWED_ORIGINS` 填写 Web 的完整来源。数据库和 API
之间应使用私有网络或 VPN，并按示例限制防火墙来源；跨不可信网络部署需配置 PostgreSQL TLS
并在 `DATABASE_URL` 使用 `sslmode=require`。

## 本机开发

API 需要可访问的 PostgreSQL DSN 和 `APP_SECRET`：

```text
set DATABASE_URL=postgresql://USER:PASSWORD@HOST:5432/DATABASE?sslmode=require
set APP_SECRET=your-stable-random-secret-at-least-32-characters
set CORS_ALLOWED_ORIGINS=http://localhost:5173
go run ./cmd/api
```

另一终端启动前端：

```text
cd web
npm ci
npm run dev
```

Windows 可直接双击根目录的 `start.bat`（内部调用 [web/start.bat](web/start.bat)，不需要
Docker）：它会检查 Node/npm 与依赖、在缺失时安装、启动开发服务器、等待服务真正就绪后打开
浏览器，并在失败时给出明确原因。若后端不是
本机 8080，把 `web/.env` 的 `VITE_DEV_API_TARGET` 指向它即可：本仓库默认已指向远端后端
`http://59.110.90.28:8080`（本机运行 Go API 时改回 `http://localhost:8080`，只经 Compose
的 Nginx 访问时填 `http://localhost`）。`VITE_API_BASE_URL` 保持留空：浏览器只访问同源
`/api`，由开发服务器代理转发，不触发跨域（远端后端未开放任何浏览器跨域来源）。前端环境
变量说明见 [web/README.md](web/README.md)。

## 数据库维护

Go 服务使用 advisory lock 应用 `internal/migrate/sql` 中的迁移；迁移失败时不会启动 HTTP
服务。Compose 启动会先等待数据库通过 `pg_isready` 健康检查。Schema 初始化和手工建库、
示例数据导入步骤见 [db/README.md](db/README.md)。

本工作区原 SQLite 历史记录已导入 PostgreSQL，并通过源端/目标端行数核对；随后已移除本地
SQLite 数据库和一次性导入工具。CSV 导出仍由 API 提供，文件导入入口已删除。

备份应通过 PostgreSQL `pg_dump` 或所用平台的备份机制完成。回滚应用时部署上一版镜像；
不要删除或重置数据库卷，也不要手工回退已执行的 schema 迁移。

## API 和检查

- Bearer token：`Authorization: Bearer <access_token>`。
- 错误体包含兼容字段 `detail` 和 `error.code/message/request_id`。
- 健康检查：`GET /api/health/live`、`GET /api/health/ready`。
- API 描述：[api/openapi.yaml](api/openapi.yaml)。

```text
go test ./cmd/api ./internal/config ./internal/domain ./internal/httpapi ./internal/migrate ./internal/repository ./internal/service
go vet ./cmd/api ./internal/config ./internal/domain ./internal/httpapi ./internal/migrate ./internal/repository ./internal/service
cd web && npm ci && npm run typecheck && npm run build
docker compose config -q
```

前端主页面仍是大型迁移文件并含 `@ts-nocheck`，OpenAPI 也未覆盖全部路由。生产环境的
备份恢复演练仍需部署方完成。
