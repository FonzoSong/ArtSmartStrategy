# 排期建议修复说明

## 功能目标与预期流程

排期建议只为当前团队的未发布内容生成建议。规划器读取项目只读规则集中的品类时长、发布时间窗和发布频率约束，并仅以同团队已发布内容作为历史表现样本。前端展示建议来源、状态、样本量和限制；编辑者可以调整、取消或采纳建议。采纳后排期写入日历，发布回填再进入历史分析。

## 涉及文件、接口和调用链路

- `frontend/index.html`：主导航入口。
- `frontend/app.js`：页面路由、内容状态筛选、排期建议发起、结果展示和错误提示。
- `frontend/styles.css`：内容状态标签样式。
- `backend/main.py`：建议 API、团队隔离、权限检查、历史数据读取、建议保存与采纳。
- `backend/scheduler/advisor.py`：无副作用的本地排期规划器。
- `backend/scheduler/rules_loader.py`：加载并校验只读规则集。
- `rules/艺数智策-内容运营规则集.json`：规则唯一数据来源。
- `tests/test_scheduler_advisor.py`、`tests/test_ai_schedule_fallback.py`：时长边界及 AI 未配置/超时回退测试。

调用链：

1. 用户打开“排期建议”，前端加载 `GET /api/recommendations` 显示最近结果。
2. 点击“AI 优先生成”或“使用规则引擎”，调用 `POST /api/ai/recommend`；本地规则按钮附带 `force_rule=true`。
3. 接口要求登录和录入权限，使用 `X-Team-Id` / 会话团队范围分别读取已发布样本与未发布目标，读取规则文件，再调用 `build_schedule_recommendations`。
4. AI 已配置时，服务端最多等待 60 秒；未配置、超时或响应异常时保留规则结果并返回可见的降级原因。
5. 接口将结果保存在 `Recommendation`，返回统一结构；前端显示建议。
6. “采纳剩余建议”调用 `POST /api/schedule/adopt`，校验日期、时段、规则和团队内容归属后写入排期。

这条链路是同步 HTTP 请求；项目没有用定时任务或队列生成排期建议。

## 当前失效表现与定位结果

确认的根因是视频时长单位不一致：内容表单明确以秒录入，示例 CSV 中的值也是秒，而规则集字段 `min_minutes` / `max_minutes` 以分钟配置。旧规划器直接将秒数与分钟数比较。例如，艺术科普上限为 6 分钟，常见的 59 秒内容会被当成 59 分钟并过滤掉。若所有未发布内容都被过滤，API 会正常返回空建议，界面看起来像“生成没有效果”。

另一个可诊断性问题是旧版前端在 API 出错时只弹出 toast，建议区域仍显示旧内容或空态，不容易看出失败发生在请求阶段。前端没有异步队列或轮询逻辑；权限拒绝、规则不可用、接口异常都会直接成为 HTTP 错误。

## 修复方案与关键代码

- `backend/scheduler/advisor.py` 将 `Content.duration` 秒数转换成分钟，再与规则上下限比较；历史数据和 API 字段结构不变。
- `frontend/app.js` 为内容记录增加“已发布 / 未发布”主标签和数量。发布分组只依据 `workflow_state === "已发布"`；其他工作流状态进入“未发布”。筛选、列表状态展示和计数使用同一映射。
- `frontend/app.js` 在生成建议时显示加载状态并禁用按钮；请求失败时在建议区域展示错误和排查方向，成功后仍使用原返回结构渲染并绑定编辑、取消和采纳操作。
- `frontend/index.html`、`frontend/app.js` 将“艺术团队管理”明确命名，并把“内容规则集”加入独立导航和页面路由。团队页保留成员添加表单；模型设置只保留模型配置；规则页复用只读规则与执行日志接口。
- 后端的团队成员创建鉴权和规则只读接口没有放宽或改写。成员添加仍要求团队负责人；规则读取仍要求有效会话。

## 验证步骤与结果

已执行：

```text
node --check frontend/app.js
# 通过

.venv/bin/pytest -q tests/test_scheduler_advisor.py tests/test_ai_schedule_fallback.py
# 9 passed
```

回归测试覆盖：分钟上限边界、超过上限的内容被过滤、AI 未配置时 120 秒（2 分钟）草稿由规则引擎生成建议，以及 AI 超时后回退规则建议。

新增 `tests/test_schedule_workflow_api.py`，使用隔离的 SQLite 内存数据库和 FastAPI 测试客户端完成“创建未发布内容 → 规则引擎推荐 → 采纳 → 检查日历记录与 `已排期` 工作流状态”完整流程；该集成测试通过。AI 超时用例使用假 HTTP 客户端，不发起网络请求。

最终全量结果：`./.venv/bin/pytest -q` 为 **106 passed、2 failed**。两项失败均在 `tests/test_example_data.py`，共同原因是 `backend/example_data.py` 需要读取的 `example/data.json` 当前不存在（目录中只有 CSV 和 schema）；此缺失与本次改动无关。页面路由、workflow 状态和设置页隔离的静态回归测试已通过。

其他检查：`node --check frontend/app.js`、针对本次 Python 文件的 Ruff 检查/格式检查、`git diff --check` 均通过。

未执行 Docker/真实 PostgreSQL 端到端测试：当前 `docker compose ps` 没有运行中的容器。集成流程仅使用独立的内存 SQLite，不会修改现有数据库。AI 服务商网络调用也未执行。

## 回滚方案

本仓库同时存在其他未提交改动，**不要对整文件执行 `git checkout` / `git restore`**，避免丢失无关工作。回滚时先保存当前差异，再只撤销本次改动：

1. 恢复 `advisor.py` 中秒到分钟转换前的过滤逻辑，以及对应的时长回归测试。
2. 恢复 `frontend/index.html` 和 `frontend/app.js` 中本次新增的规则导航/页面、团队标题、内容状态标签和排期请求反馈；恢复 `frontend/styles.css` 的 `.asset-status-tabs` 样式。
3. 删除本说明文档。
4. 不需要也不应重置数据库或迁移版本。

## 后续排查建议

- 若生产环境仍没有建议，先查看 `/api/ai/recommend` 响应中的 `analysis_status`、`fallback_reason`、`data_summary` 和 HTTP 状态码。
- 检查当前团队是否有符合规则品类/时长的未发布内容，并确认其 `workflow_state` 与 `lifecycle` 字段合理。
- 检查 `rules/艺数智策-内容运营规则集.json` 是否存在且通过校验；规则页应显示规则与最近执行日志。
- 若显示权限错误，确认账号角色（负责人或录入者）及请求的团队上下文。查看者不能生成、修改或采纳建议。
- 若 AI 路径失败，检查模型 Base URL、模型名称和服务器端密钥配置；规则引擎路径不依赖 AI 服务。
- 如果需要真实部署验证，使用隔离测试团队执行“创建未发布内容 → 规则生成 → 采纳 → 日历核对”，不要对生产数据执行清库或迁移重置。
