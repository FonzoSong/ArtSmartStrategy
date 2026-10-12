# 排期建议排查说明

## 预期流程

1. 登录后，前端请求 `POST /api/ai/recommend` 生成未发布内容建议。
2. Go 服务从 PostgreSQL 读取当前团队的未发布候选、已发布历史样本和数据库规则集。
3. AI 未配置、失败或超时后，服务使用本地规则引擎生成同结构结果，并返回来源和降级原因。
4. 用户在前端检查后，通过 `POST /api/schedule/adopt` 采纳建议；手工排期使用
   `POST /api/schedule/manual`。
5. 发布后通过排期发布接口保存结果，后续统计只使用已发布内容。

## 调用链

- UI 和请求状态：`web/src/app.ts`
- API 请求、错误解析、API 地址：`web/src/api/client.ts`
- 路由和授权：`internal/httpapi/server.go`
- 推荐规则逻辑：`internal/service/advisor.go`、`internal/service/rules.go`
- PostgreSQL 查询和写入：`internal/repository/content.go`、
  `internal/repository/schedule.go`
- 规则初始数据：`internal/migrate/sql/0002_seed_rules.sql`

推荐请求只会读取当前会话团队的数据，并仅以已发布内容作为历史样本。AI 调用有 60 秒
上限；失败原因写入响应，调试时也可检查 API 容器结构化日志及 `GET /api/health/ready`。

## 排查步骤

1. 检查 Compose：`docker compose ps`，确认 `api` 健康、`web` 已启动。
2. 查看服务日志：`docker compose logs --tail=200 api`。
3. 确认 `.env` 的 PostgreSQL 地址、团队账号和规则迁移状态正确；不要将密钥复制到日志或工单。
4. 浏览器开发者工具查看 `POST /api/ai/recommend` 的状态码和响应中的 `source`、
   `analysis_status`、`fallback_reason`、`sample_count`。
5. 若建议不足或为空，检查当前团队是否有未发布候选、同类已发布样本及可用的数据库规则集。
6. 若写入/采纳失败，检查操作者角色、内容所属团队以及数据库事务错误。

## 回滚和数据保护

应用代码回滚到上一版镜像即可；不要删除或重置 PostgreSQL 数据卷。Schema 采用向前迁移，
需要回退数据库时使用托管数据库快照/PITR，并先恢复到隔离实例验证。不要把本地规则文件
作为线上临时回退来源。

当前环境没有可用的云 PostgreSQL DSN，数据库集成、AI 联网调用和完整浏览器流程尚未实际
验证；运行单元检查不等于完成这些验收。
