# 艺数智策

艺术团队内容运营系统，采用 Go REST API、独立 TypeScript/Vite 前端和
Docker Compose PostgreSQL。浏览器仅通过同源 `/api` 访问后端；数据库只在内部网络
接受 API 和运维连接。

## 架构

- `cmd/api`：Go 服务入口、连接池、有限重试、Schema 迁移与优雅关闭。
- `internal/httpapi`：REST API、认证、权限、CORS 和统一错误响应。
- `internal/repository`：PostgreSQL 查询、团队隔离和事务。
- `internal/migrate/sql`：版本化 PostgreSQL schema 迁移。
- `web`：独立 Vite/TypeScript 应用；生产由 Nginx 代理 `/api/`。
- `db/init`、`db/sql`：首次建库 schema 和独立的可选示例 SQL。

业务数据持久化在 PostgreSQL named volume `arts-smart-postgres-data`。服务不能连接数据库
时会有限重试后失败，不会改用本地文件。项目不再包含本地业务数据库或运行时文件导入 API。

## 配置和启动

安装 Docker Desktop（含 Compose v2），将 `.env.example` 复制为 `.env`，配置
`POSTGRES_ADMIN_PASSWORD`、`APP_DB_PASSWORD` 和至少 32 字符的 `APP_SECRET`。可选配置
`AI_API_KEY`；密钥只从后端环境注入，不会回显给浏览器。

```text
docker compose config -q
docker compose up --build --detach --wait
docker compose ps
docker compose logs -f api
```

Windows 可运行 `start.bat`，其执行相同的 Docker Compose 启动流程。Web 默认地址为
`http://localhost:8081`；API 经同源 `/api` 代理访问。PostgreSQL 使用 `db` 服务名，
仅加入内部 `db-net`，没有宿主机端口映射。API 运行账号是非超级用户。

`LOAD_EXAMPLE_DATA=true` 仅控制数据库第一次初始化时是否执行
`db/sql/02_seed_data.sql`。新环境默认导入虚构样例；现有数据库卷不会再次执行初始化脚本。
该 SQL 可幂等重跑，但不会自动随容器重启执行。不要运行 `docker compose down -v`，除非
明确要删除数据库卷及其中全部数据。

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
