# 艺数智策使用说明

艺数智策是面向艺术团队的内容运营复盘网站。它使用团队主动录入的数据分析内容表现、形成排期建议并记录发布后的反馈。应用以 PostgreSQL 保存业务数据，面向 Windows 提供批处理启动脚本，也支持 Docker Compose 部署。数据库可用时首次启动只执行迁移、不写入演示业务数据；数据库不可用时会明确进入临时示例数据模式。

项目采用 Apache-2.0 许可证。开发者可通过 `pip install -e ".[dev]"` 安装开发依赖，使用 Ruff、mypy 和 pytest 检查代码；贡献流程见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## Windows 本地启动

准备 Windows 10/11、Python 3.10 或更新版本及 Python Launcher（`py` 命令），并安装 PostgreSQL 16 或兼容版本。安装 PostgreSQL 时记下端口、用户名和密码，并确保数据库服务已启动。

1. 在项目目录复制 `.env.example` 并命名为 `.env`。也可以直接双击 `start.bat`，它会首次创建 `.env` 并打开记事本。
2. 修改 `.env` 中的 `DATABASE_URL`，填写本机 PostgreSQL 用户、密码、地址和数据库名。
3. 保存 `.env` 后双击 `start.bat`。脚本会创建 Python 虚拟环境、安装依赖并启动网站；数据库可连接时先执行迁移并使用 PostgreSQL，连接失败时改从项目根目录的示例 JSON 启动。默认不注入测试数据。显式 `start.bat --seed` 仍要求可用的 PostgreSQL 数据库。
4. 浏览器访问 <http://127.0.0.1:8000>。首次访问时创建团队负责人账号。

持久化数据库连接只支持 PostgreSQL，不再接受 SQLite 连接串。示例模式内部使用进程内临时数据库实现可交互演示，不会被当作持久化 PostgreSQL。
项目中的 `data/arts_smart.db` 是历史 SQLite 文件，当前应用不会读取它；删除该文件不会重置 PostgreSQL 数据库。Docker PostgreSQL 数据存放在项目目录的 `postgres-data` 持久化目录中。

示例连接串：

```env
DATABASE_URL=postgresql+psycopg://postgres:change-this@localhost:5432/arts_smart
APP_SECRET=replace-this-for-a-shared-deployment
```

密码含 `@`、`:`、`/`、`#` 等字符时，请先进行 URL 编码。依赖安装需要网络；启动后即使不配置模型，也可使用规则分析功能。若 `.env` 缺少 `APP_SECRET` 或仍是模板占位值，应用会使用系统时间哈希与密码学随机熵生成密钥，并通过临时文件和原子替换写回 `.env`。已有有效密钥不会被覆盖；共享/生产环境应由运维注入强随机 `APP_SECRET` 并保护 `.env`。

### PostgreSQL 模式与示例数据模式

数据库连接成功且迁移完成时，页面/API 从 PostgreSQL 读写，修改具备持久性。数据库未配置或连接不可用时，应用从项目根目录的 `example/data.json`（由 `example/schema.json` 校验）加载本地示例数据并启动；页面会显示“示例数据模式”，修改仅保存在当前进程的临时演示库，重启后从 JSON 文件恢复。示例模式不会写入、覆盖或回填 PostgreSQL。数据库恢复后，重启应用即可重新尝试迁移并切回 PostgreSQL 模式。若数据库可连接但 schema 缺失、迁移失败或版本不匹配，应用会明确失败，不会用示例模式掩盖数据库结构错误。示例模式用户、团队和演出关系由 `example/data.json` 提供，可使用文件中的负责人邮箱及 `demo_password` 登录；凭据仅用于本地演示。请勿把示例模式当作生产存储。

## Docker 部署

需要 Docker Desktop，并确认已启用 Linux 容器。复制 `.env.example` 为 `.env`，至少修改 `POSTGRES_PASSWORD`；共享或生产部署应手动注入 `APP_SECRET`，本地模板占位值会自动生成并持久化，然后在项目目录打开 Windows 终端运行：

```bat
docker compose up --build
```

服务启动后访问 <http://localhost:8000>。PostgreSQL 数据保存在项目目录的 `postgres-data` 持久化目录，容器重建后仍保留。停止服务可在运行窗口按 Ctrl+C；后台启动可执行 `docker compose up --build -d`，停止容器但保留数据库可执行 `docker compose down`。如需清空数据库目录，请先备份，因为删除目录会永久删除其中数据。

Compose 保留一个独立的一次性迁移任务，应用等待迁移任务结束，但迁移任务只等待数据库容器启动、不等待 PostgreSQL 健康检查。连接失败时有限重试后以可降级状态结束，应用随后从本地 JSON 启动；迁移脚本或可连接数据库的 schema 错误仍会阻止应用启动。这样同一 Compose 项目的应用副本不会各自并发执行迁移。PostgreSQL 继续保留 `pg_isready` 健康检查，`db` 是 Compose 网络内的稳定主机名。数据库恢复后运行 `docker compose run --rm migrate`，再运行 `docker compose restart artsmart`，即可重新迁移并切换到 PostgreSQL。Docker 自动生成的 `APP_SECRET` 保存在独立的 `app-secret` 命名卷中；`docker compose down` 会保留它和数据库卷。不要使用 `docker compose down -v`，否则会删除这两个持久化卷。

## 首次登录和测试数据

首次数据库模式访问时创建团队名称、负责人姓名、邮箱和至少 10 位密码。正常数据库启动不会自动创建示例团队或内容。若要显式注入 120 条业务示例内容，可运行 `start.bat --seed`；数据库已有内容时脚本会先询问确认。`start.bat --seed --force` 跳过确认。重复运行通过团队、内容和关联演出的稳定 ID 识别已注入数据，不会重复添加。运行时示例模式与数据库测试数据注入是两个独立模式。

Docker 默认不注入测试数据。确认需要测试数据后，在 `.env` 中设置 `SEED_DATA=true` 并启动 Compose；注入操作可重复执行。测试数据来源于 [`example/data.json`](example/data.json)，包含 3 个团队、120 条内容、演出、Tag 和自动/手动排期关系；业务代码不包含演示记录。

查看者账号只能读取数据，不能写入。负责人可在独立的“团队管理”页面邀请录入者或查看者。各团队数据按团队隔离。

## 主要功能

- 运营总览：内容量、趋势、平台表现、优质内容、数据完整度、采纳收益和转化漏斗。运营效果统计只纳入“已发布”内容，未发布资产仍可在内容记录页检索和导出。
- 内容记录：演出预订与宣发内容通过独立实体关联；支持负责人分配和待制作、已制作、待排期、已排期、已发布、未发布工作流状态；状态筛选统计来自 `workflow_state`，发布表现仍只纳入发布状态为“已发布”的资产。可推进制作状态、手动排期、选择 Tag、CSV 导入/导出及筛选分页。
- 效果分析：内容类型与平台对比、星期及时段表现、团队历史基准、综合分权重、转化漏斗和评论关键词提示。
- 排期建议：仅为未发布资产排期，并由项目只读规则集约束；模型配置用于自然语言洞察和连接测试。
- 排期日历：采纳建议、编辑日期、记录发布结果并查看预测偏差。
- 报告与交接：导出 PDF 复盘报告、脱敏 CSV、排期 CSV 和 Markdown 交接包。
- 团队管理：独立页面管理团队名称、成员角色、邀请和操作记录；查看者只读，写入权限由服务端校验。
- Tag 库：维护团队标签，支持名称规范化去重、使用次数、重命名、合并和停用；停用不删除历史内容关系。
- 内容规则集：内容校验规则按团队存储于 PostgreSQL，可增删改查、按优先级启停、试运行并在保存内容时执行；更新人和更新时间记入数据及审计日志。排期策略仍从 [`rules/艺数智策-内容运营规则集.json`](rules/艺数智策-内容运营规则集.json) 只读加载。

总览、趋势和表现分析只统计“已发布”内容；“未发布”内容不混入表现汇总，只作为排期建议目标和交接待办。排期建议依据规则文件中的品类时长、平台、日期、时段及频率限制，并引用同品类/平台已发布内容的播放、点赞、收藏、评论和完播数据。数据库将发布生命周期限制为“已发布”或“未发布”，工作流通过独立枚举表达制作与排期过程，并在 `content_workflow_event` 中记录流转时间、操作人和来源。

本版本不含图片识别。所有导出通过已登录的应用请求下载，并依据响应头保存为对应的 PDF、CSV 或 Markdown 文件名。

## 模型接口设置

模型接口设置只配置 OpenAI-compatible 服务商的 Base URL、模型名称和 API Key，并可测试连接。默认模型名称为 `qwen3.8-max`，默认 Base URL 从后端配置 `AI_BASE_URL` 读取，`.env.example` 示例为 `https://api.kunyou.asia/v1`；模型设置和 AI 请求共用该配置，调用拼接 `/chat/completions` 时不会重复添加版本段。地址与密钥通过 `.env` 的 `AI_BASE_URL`、`AI_API_KEY` 或受保护的设置 API 注入。API Key 不会回显；留空会保留服务器已保存密钥。AI 排期仅发送当前团队同类型、同平台的已发布表现样本；未配置、错误或 60 秒超时会自动回退本地规则引擎，并显示降级原因。启用远程模型前，请按服务商的数据处理政策评估数据传输。

## 数据库迁移与自测

启动脚本与 Compose 共用 `backend.container_entrypoint` 作为迁移入口。若需手动查看或执行迁移，在 Windows 命令提示符运行：

```bat
.venv\Scripts\python.exe -m alembic current
.venv\Scripts\python.exe -m backend.container_entrypoint migrate
```

端到端自测使用当前配置的 PostgreSQL 数据库，请使用专用测试库或测试环境，完成依赖安装后运行：

```bat
.venv\Scripts\python.exe scripts\selftest.py
```

认证专项测试：

```bat
.venv\Scripts\python.exe scripts\test_auth.py
```

重置测试/开发数据库（会清空 `public` schema，需输入 `RESET` 确认）：

```bash
bash reset.sh
```

本地 Python 环境会直接连接 `.env` 中的 PostgreSQL；如果没有 `.venv` 但检测到 Docker Compose，重置脚本会删除并重建项目内的 `postgres-data` 持久化目录，再用本地已有镜像启动服务，不拉取镜像、不需要外网。Windows 可在 Git Bash 或 WSL 中执行；生产数据库请先备份再使用。

通过时会显示检查通过的总数。API 健康状态位于 `/api/health`，交互式 API 文档位于 `/docs`。

## 数据与安全

请定期备份 PostgreSQL，并将备份放在独立于应用目录的位置。团队网络部署应设置强随机 `APP_SECRET`、限制数据库和应用端口，并通过 HTTPS 反向代理访问。不要录入观众个人隐私信息，也不要将 `.env` 文件分享给他人。


## CSV 字段说明

内容导入模板提供现有业务字段：平台、发布时间、标题、内容类型、时长、播放/互动数据、看完比例、状态标签、标签、素材说明、责任人、备注、转化数据、评论摘录、关联演出和工作流状态。必填字段为平台、发布时间、标题、播放量、点赞量；状态标签仅允许“已发布”或“未发布”，省略时按“已发布”导入。关联演出可填写当前团队的演出名称，负责人可填写当前团队成员姓名或邮箱。工作流状态允许“待制作”“待排期”“已排期”“已发布”；状态与工作流不一致时导入会提示错误。模板包含一条示例记录。

内容记录页支持关键词搜索、平台/类型/状态筛选、排序、分页和批量操作；查看者保留只读权限。排期日历默认打开当前月份，月历与月清单展示同一月份的计划。
