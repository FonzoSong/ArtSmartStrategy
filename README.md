# 艺数智策

艺数智策是一个艺术团队内容运营系统。当前重构版本采用 Go REST API、独立
TypeScript/Vite 前端，以及外部托管 PostgreSQL。浏览器只访问 API，不连接数据库。

## 架构

- `cmd/api`：Go 服务入口、连接池、有限数据库重试、Schema 迁移和优雅关闭。
- `internal/httpapi`：REST API、认证、角色访问控制、CORS 和统一错误响应。
- `internal/service`：认证、兼容旧 PBKDF2 密码格式、会话 token 和凭据加密。
- `internal/repository`：PostgreSQL 数据访问、团队隔离和事务。
- `internal/migrate/sql`：版本化 PostgreSQL schema 与只读规则集导入。
- `web`：独立 Vite/TypeScript 应用；API 地址由 `VITE_API_BASE_URL` 管理。

现有 Python 后端和旧静态前端保留在仓库中，作为迁移期间对照与回滚来源。新 Go
服务不读取本地 JSON/CSV/SQLite 作为运行时业务存储，数据库连接失败时会有限重试后
失败退出，不会切换到本地数据。

## 前置配置

1. 准备 PostgreSQL 托管数据库，并确保部署环境可以通过 TLS 连接。
2. 复制 `.env.example` 为 `.env`，填入数据库 URL 和稳定的随机 `APP_SECRET`。
3. 把 `CORS_ALLOWED_ORIGINS` 配置成实际前端地址。
4. 如启用模型功能，设置 `AI_API_KEY`；密钥只从环境注入，API 不回显。

`APP_SECRET` 可使用密码管理器生成，或在 POSIX 管理机运行 `openssl rand -base64 48`。
请勿提交 `.env` 或真实凭据，也不要在不同实例间更换已有密钥，否则现有会话失效。

## 启动

### Docker Compose

Compose 不创建本地数据库容器或数据库卷；它连接 `.env` 中的托管 PostgreSQL。

```text
docker compose up --build -d
docker compose ps
docker compose logs -f api
```

前端默认位于 `http://localhost:8081`，API 位于 `http://localhost:8080`。
首次 API 启动会用数据库 advisory lock 串行应用嵌入式迁移，迁移失败时不启动 HTTP 服务。

### 本机开发

终端一（API）：

```text
set DATABASE_URL=postgresql://USER:PASSWORD@HOST:5432/DATABASE?sslmode=require
set APP_SECRET=your-stable-random-secret-at-least-32-characters
set CORS_ALLOWED_ORIGINS=http://localhost:5173
 go run ./cmd/api
```

终端二（前端）：

```text
cd web
npm ci
npm run dev
```

Windows 可先编辑 `.env` 后运行 `start.bat`；它使用 Docker Compose 启动，不使用 shell 脚本。

## 数据库迁移

Go 服务启动时执行 `internal/migrate/sql` 的版本迁移。当前迁移能从已知 Alembic 头
`0006_workflow_rules_tags` 安全接管兼容 Schema；对未知迁移版本或未跟踪 Schema 会拒绝
启动。迁移前应在托管数据库侧创建快照，并验证提供方的恢复流程。

### SQLite 历史数据导入

导入工具默认只读检查，要求目标数据库已经完成 schema migration 且所有业务表为空。
先在隔离目标或刚创建的空云数据库执行：

```text
set DATABASE_URL=postgresql://USER:PASSWORD@HOST:5432/DATABASE?sslmode=require
go run ./tools/migrate_sqlite.go --source data/arts_smart.db
go run ./tools/migrate_sqlite.go --source data/arts_smart.db --apply
```

工具打印源表行数，事务导入并核验目标行数，调整 serial sequence。源 SQLite 文件始终
保留；导入前先做只读备份。不要对已有业务数据的目标库使用此导入工具。导入失败时
事务回滚，修复后从空目标重试；若提交后发现数据问题，恢复托管数据库快照或回滚到旧
应用读取源库，不要删除源库文件。

## API 契约

- JSON 请求/响应使用 UTF-8，分页和错误字段逐步按既有 API 兼容。
- 鉴权使用 `Authorization: Bearer <access_token>`。
- 错误体包含兼容字段 `detail` 以及 `error.code/message/request_id`。
- 健康检查：`GET /api/health/live` 和 `GET /api/health/ready`。
- OpenAPI 描述文件位于 [api/openapi.yaml](api/openapi.yaml)。它覆盖认证、内容、团队、Tag
  与统计等基础契约；排期、规则、模型配置、导入导出等路由仍需补全到该文件。

## 构建与检查

```text
go test ./cmd/api ./internal/config ./internal/domain ./internal/httpapi ./internal/migrate ./internal/repository ./internal/service ./tools
go vet ./cmd/api ./internal/config ./internal/domain ./internal/httpapi ./internal/migrate ./internal/repository ./internal/service ./tools
cd web && npm run typecheck && npm run build
```

Go 测试按源码包显式列出，避免扫描仓库中权限受限的旧 `postgres-data/` 卷。
需要真实数据库的导入和启动验证必须使用隔离的托管测试库；不要用生产库验证迁移。

## 备份、恢复与回滚

- 用数据库提供方的自动备份/PITR 或快照保护数据，定期演练恢复到隔离实例。
- Schema 迁移采用向前兼容的新增式变更；部署回滚使用上一版应用镜像。不要手工
  删除迁移历史或回退已提交的 DDL。
- 若新服务尚未写入数据，可切回旧 Python 部署并继续使用原数据库；新服务写入后，先
  评估 Go Schema 和旧应用的双向兼容，再切换。不要让两版同时执行相同数据库写入。
- SQLite 源数据仅在云端导入核验并完成备份恢复演练后，才由维护者另行归档。

## 当前迁移范围

Go API 当前实现了认证与角色权限、团队/成员、演出、内容生命周期、Tag、规则 CRUD 与执行、
模型配置/连接测试、推荐生成与采纳、手动/自动排期记录、发布结果回填、统计、审计、分析、
CSV 导入导出和交接/报告导出。核心新运行路径不读取本地业务 JSON/CSV/SQLite 文件。

本次迁移尚未达到完整切换验收：

- `web/src/app.ts` 是从现有页面迁移来的大型单文件页面，带有 `@ts-nocheck`；Vite 构建和
  独立 TS 类型模块检查通过，但页面逻辑尚未拆成严格类型化的组件/状态/API 层。
- OpenAPI 文件尚未列出全部 Go 路由和每个接口的完整请求/响应 Schema。
- 没有可用的云 PostgreSQL 测试 DSN；真实 schema 应用、SQLite 导入、并发/重启持久性和
  备份恢复均未验证。导入工具只对隔离的空目标库执行 `--apply`。
- 旧 Python 服务与静态页面暂时保留供核对和回滚。尚未删除旧文件读取/本地数据库逻辑；新 Go
  服务及新前端部署不使用这些旧运行路径。

因此当前可以验证 Go/API 与前端构建，不应把生产数据切换或完整旧功能兼容描述为已验收。
