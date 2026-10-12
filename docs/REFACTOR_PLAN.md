# Go + TypeScript 重构审计与实施计划

## 当前架构审计

- 后端：Python 3.14、FastAPI、SQLAlchemy、Alembic；`backend/main.py` 集中处理认证、团队、内容、规则、排期、AI、分析、导入导出和报告接口。
- 数据层：PostgreSQL 是当前正式存储，现有 ORM 涵盖团队、账号/会话、内容、演出、Tag、规则、排期、推荐、分析基准、日志和工作流事件。共有 6 个 Alembic 版本。
- 前端：原生 HTML/CSS/JavaScript 单页应用。`frontend/app.js` 同时承担页面渲染、状态管理、权限显示、API 请求及表单操作。
- 本地数据：仓库含 `data/arts_smart.db`。只读清点为 13 条内容、2 个团队、2 个用户、3 个会话、3 条排期、2 条推荐、8 条审计日志、1 个模型配置和 1 个分析基准。该 SQLite 文件不是当前应用正式数据库，但属于需要保全的潜在迁移来源。
- 配置与静态业务文件：`.env` 当前数据库 hostname 为 localhost；`example/data.json` 和 `rules/艺数智策-内容运营规则集.json` 供示例模式/排期策略读取。切换后动态策略应入云库；示例 JSON 不再作为新运行时的业务数据源。
- 部署：当前 Compose 内置 PostgreSQL 容器，应用和前端由 Python 容器提供；`start.bat` 负责 Windows 本地启动。
- 目标数据库：环境未配置可确认的云数据库 DSN。本轮实现采用 PostgreSQL 协议兼容的外部 `DATABASE_URL`，不假定任何云供应商，也不执行真实数据迁移。

## 目标目录与边界

```text
cmd/api/                 Go 服务入口
internal/config/         环境配置与校验
internal/domain/         核心实体与业务约束
internal/repository/     PostgreSQL 数据访问
internal/service/        业务用例
internal/httpapi/        REST API、认证中间件、错误映射
internal/migrate/        SQL schema 迁移
web/src/                 TypeScript 页面、状态、API 客户端和领域类型
web/public/              静态资源
tools/                   一次性 SQLite -> PostgreSQL 数据迁移工具
```

Go API 通过版本化 JSON 契约通信，API 错误统一为 `{ "error": { "code", "message", "request_id" } }`。前端只依赖 API，不接触数据库 DSN。

## 阶段与风险控制

1. **契约与数据库**：冻结现有实体/枚举/路由，建立云库 schema、索引、约束和应用版本迁移。兼容当前 Alembic head 的 PostgreSQL 数据结构，不改写历史迁移。
2. **Go 服务框架**：环境配置、结构化日志、超时、连接池、事务、健康检查、CORS、统一错误和认证授权。
3. **业务接口迁移**：按认证/团队/演出、内容/Tag/规则、排期/建议/发布回填、统计/导出/假设/交接分组迁移。关键业务需保留团队隔离和查看者只读。
4. **TypeScript 前端**：复用既有交互与样式，迁移到独立 Vite 应用；集中 API 地址、会话、错误与业务类型。
5. **数据迁移**：先只读导出与校验 SQLite 数据，再事务导入云库；比较每表数量、主外键和关键指标。迁移成功前保留源数据库文件。
6. **切换与运维**：独立构建/部署前后端，Compose 不再包含业务数据库容器；增加备份恢复、健康检查、部署/回滚文档。

### 风险与处理

- **账号密码兼容**：旧账号使用 PBKDF2-SHA256 310,000 次。Go 验证器先兼容旧格式，不能验证旧 hash 的账号必须要求安全重置，不能静默重置密码。
- **旧 SQLite schema 较旧**：导入按列白名单和显式默认值处理，逐表记录拒绝原因；事务失败整体回滚，源库只读保留。
- **云服务商未知**：仅采用标准 PostgreSQL TLS DSN；正式迁移前需在部署环境提供云库 DSN、TLS 与网络访问。
- **大范围前后端切换**：回滚使用上一版 Go 镜像；保留原数据库数据和迁移工具，不重置数据库。
- **本地规则文件**：将现行排期策略转为有版本的数据库配置迁移，保留只读归档以供审计，不再让线上业务读取文件。

## 当前执行记录

- [x] 仓库、业务模块、API、依赖、迁移、启动入口与本地文件依赖审计。
- [x] SQLite 历史业务数据只读计数清点。
- [x] Go 后端框架、外部 PostgreSQL Schema、认证、团队/成员、内容、工作流、演出、Tag、规则、AI 推荐、排期、发布回填、统计、审计和导出 API 已实现。
- [~] 独立 Vite/TypeScript 应用、统一 API 客户端和共享核心类型已建立；页面逻辑目前仍是迁移来的大型单文件 `app.ts`，包含 `@ts-nocheck`，尚未完成组件拆分和严格类型覆盖。
- [~] SQLite -> PostgreSQL 只读 dry-run / 空库事务导入工具已实现，包含源表/目标行数校验和 sequence 校正；真实云库导入未执行。
- [~] Compose 已改为外部 PostgreSQL 的 API + Web 部署；无云端 DSN，因此云端部署、备份恢复及数据库集成验证未执行。
- [~] OpenAPI 已提供基础契约，尚未覆盖全部 API 和详尽 Schema。
- [x] 清理旧 Python 应用、静态页面、Alembic 执行配置、Python 依赖和旧 Python 测试；本地 SQLite 数据库保留为待显式迁移的数据源。

## 本轮检查

- `go test ./cmd/api ./internal/config ./internal/domain ./internal/httpapi ./internal/migrate ./internal/repository ./internal/service ./tools`：通过。
- 同一显式包列表执行 `go vet`：通过。
- `go mod verify`：通过。
- `cd web && npm run typecheck && npm run build`：通过；类型检查不覆盖带 `@ts-nocheck` 的旧页面逻辑。
- `docker compose config -q`：通过，使用临时占位环境变量，仅校验 Compose 展开，不连接数据库。
- PostgreSQL 启动/迁移、历史数据导入和数据库恢复未执行，因为当前环境未提供隔离的云 PostgreSQL DSN。
