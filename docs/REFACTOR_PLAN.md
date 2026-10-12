# Go + TypeScript 重构审计与实施计划

## 当前架构审计

- 后端：Go REST API，覆盖认证、团队、内容、排期、规则、AI、统计和导出。
- 数据层：PostgreSQL，使用版本化 SQL 迁移和团队隔离查询。
- 前端：独立 Vite/TypeScript 应用，通过 API 访问业务数据。
- 历史数据：工作区原 SQLite 含 13 条内容、2 个团队、2 个用户、3 个会话、3 条排期、2 条推荐、8 条审计日志、1 个模型配置和 1 个分析基准；已导入 PostgreSQL 命名卷并按行数核验，随后删除本地数据文件。
- 部署：Compose 管理 Web、Go API 和 PostgreSQL；`start.bat` 调用 Compose，不启动旧 Python 服务。

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
```

Go API 通过版本化 JSON 契约通信，API 错误统一为 `{ "error": { "code", "message", "request_id" } }`。前端只依赖 API，不接触数据库 DSN。

## 阶段与风险控制

1. **契约与数据库**：冻结现有实体/枚举/路由，建立云库 schema、索引、约束和应用版本迁移。兼容当前 Alembic head 的 PostgreSQL 数据结构，不改写历史迁移。
2. **Go 服务框架**：环境配置、结构化日志、超时、连接池、事务、健康检查、CORS、统一错误和认证授权。
3. **业务接口迁移**：按认证/团队/演出、内容/Tag/规则、排期/建议/发布回填、统计/导出/假设/交接分组迁移。关键业务需保留团队隔离和查看者只读。
4. **TypeScript 前端**：复用既有交互与样式，迁移到独立 Vite 应用；集中 API 地址、会话、错误与业务类型。
5. **数据迁移**：只读盘点 SQLite 数据，在 PostgreSQL 事务导入并比较每表数量；核验后移除本地源文件和一次性迁移工具。
6. **切换与运维**：独立构建/部署前后端，Compose 提供隔离数据库服务、named volume、健康检查和部署/回滚文档。

### 风险与处理

- **账号密码兼容**：旧账号使用 PBKDF2-SHA256 310,000 次。Go 验证器先兼容旧格式，不能验证旧 hash 的账号必须要求安全重置，不能静默重置密码。
- **旧 SQLite schema 较旧**：导入按列白名单和显式默认值处理，逐表记录拒绝原因；事务失败整体回滚，源库只读保留。
- **云服务商未知**：本地 Compose 使用独立 PostgreSQL；外部部署采用标准 PostgreSQL DSN，需部署方设置 TLS 和网络访问。
- **大范围前后端切换**：回滚使用上一版 Go 镜像；数据库 named volume 和迁移历史不重置。
- **本地规则文件**：将现行排期策略转为有版本的数据库配置迁移，保留只读归档以供审计，不再让线上业务读取文件。

## 当前执行记录

- [x] 仓库、业务模块、API、依赖、迁移、启动入口与本地文件依赖审计。
- [x] SQLite 历史业务数据只读计数清点。
- [x] Go 后端框架、外部 PostgreSQL Schema、认证、团队/成员、内容、工作流、演出、Tag、规则、AI 推荐、排期、发布回填、统计、审计和导出 API 已实现。
- [~] 独立 Vite/TypeScript 应用、统一 API 客户端和共享核心类型已建立；页面逻辑目前仍是迁移来的大型单文件 `app.ts`，包含 `@ts-nocheck`，尚未完成组件拆分和严格类型覆盖。
- [x] SQLite -> PostgreSQL 事务迁移已在当前数据库执行并完成源/目标行数核对；临时迁移工具和源文件随后清理。
- [x] Compose 提供 PostgreSQL、API、Web 三个服务，DB 网络隔离并持久化至 named volume。
- [~] OpenAPI 已提供基础契约，尚未覆盖全部 API 和详尽 Schema。
- [x] 清理旧 Python 应用、静态页面、Alembic 执行配置、Python 依赖、旧 Python 测试及 SQLite 业务文件。

## 本轮检查

- `go test` 和 `go vet` 使用不含 SQLite 导入工具的 Go 包列表运行。
- `go mod verify`：通过。
- `cd web && npm run typecheck && npm run build`：通过；类型检查不覆盖带 `@ts-nocheck` 的旧页面逻辑。
- `docker compose config -q`：验证 Compose 结构；本机 PostgreSQL 容器健康，历史数据迁移完成。
- Docker 镜像全量构建受镜像仓库网络访问限制；生产备份恢复演练需部署环境执行。
