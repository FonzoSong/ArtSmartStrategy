# 艺数智策使用说明

艺数智策是面向艺术团队的内容运营复盘网站。它使用团队主动录入的数据分析内容表现、形成排期建议并记录发布后的反馈。应用以 PostgreSQL 保存业务数据，面向 Windows 提供批处理启动脚本，也支持 Docker Compose 部署。首次初始化会写入 3 条模拟记录；删除后不会在重启时自动恢复。

项目采用 Apache-2.0 许可证。开发者可通过 `pip install -e ".[dev]"` 安装开发依赖，使用 Ruff、mypy 和 pytest 检查代码；贡献流程见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## Windows 本地启动

准备 Windows 10/11、Python 3.10 或更新版本及 Python Launcher（`py` 命令），并安装 PostgreSQL 16 或兼容版本。安装 PostgreSQL 时记下端口、用户名和密码，并确保数据库服务已启动。

1. 在项目目录复制 `.env.example` 并命名为 `.env`。也可以直接双击 `start.bat`，它会首次创建 `.env` 并打开记事本。
2. 修改 `.env` 中的 `DATABASE_URL`，填写本机 PostgreSQL 用户、密码、地址和数据库名；将 `APP_SECRET` 改成较长的随机字符串。数据库用户需要创建数据库的权限，首次运行脚本会在数据库不存在时尝试创建。
3. 保存 `.env` 后双击 `start.bat`。脚本会创建 Python 虚拟环境、安装依赖、准备数据库、应用迁移并启动网站。
4. 浏览器访问 <http://127.0.0.1:8000>。首次访问时创建团队负责人账号。

应用运行、登录注册和测试脚本均只支持 PostgreSQL，不再接受 SQLite 连接串。
项目中的 `data/arts_smart.db` 是历史 SQLite 文件，当前应用不会读取它；删除该文件不会重置 Docker 数据库。Docker 数据实际存放在 `postgres-data` 命名卷中。

示例连接串：

```env
DATABASE_URL=postgresql+psycopg://postgres:change-this@localhost:5432/arts_smart
APP_SECRET=请替换为随机长字符串
```

密码含 `@`、`:`、`/`、`#` 等字符时，请先进行 URL 编码。依赖安装需要网络；启动后即使不配置模型，也可使用规则分析功能。

## Docker 部署

需要 Docker Desktop，并确认已启用 Linux 容器。复制 `.env.example` 为 `.env`，至少修改 `APP_SECRET` 和 `POSTGRES_PASSWORD`，然后在项目目录打开 Windows 终端运行：

```bat
docker compose up --build
```

服务启动后访问 <http://localhost:8000>。PostgreSQL 数据保存在 Docker 命名卷 `postgres-data`，容器重建后仍保留。停止服务可在运行窗口按 Ctrl+C；后台启动可执行 `docker compose up --build -d`，停止容器但保留数据库可执行 `docker compose down`。如需清空数据库卷，请先确认备份，因为删除卷会永久删除其中数据。

## 首次登录和模拟数据

首次访问创建团队名称、负责人姓名、邮箱和至少 10 位密码。初始化时会创建 3 条模拟内容记录，供仪表盘、分析和排期建议演示。它们只是演示样本，不代表真实运营结果。团队后续可手动录入、编辑或删除内容；删除后不会出现恢复示例数据的入口，也不会在应用重启时重新载入。

查看者账号只能读取数据，不能写入。负责人可在设置中添加录入者或查看者。各团队数据按团队隔离。

## 主要功能

- 运营总览：内容量、趋势、平台表现、优质内容、数据完整度、采纳收益和转化漏斗。运营效果统计只纳入“已发布”内容，未发布资产仍可在内容记录页检索和导出。
- 内容记录：手动录入、CSV 批量导入/导出、去重提示、标签、素材说明、负责人、备注及“已发布 / 未发布”状态管理。
- 效果分析：内容类型与平台对比、星期及时段表现、团队历史基准、综合分权重、转化漏斗和评论关键词提示。
- 排期建议：基于团队数据生成建议；未配置模型或调用失败时，可切换到规则分析。
- 排期日历：采纳建议、编辑日期、记录发布结果并查看预测偏差。
- 报告与交接：导出 PDF 复盘报告、脱敏 CSV、排期 CSV 和 Markdown 交接包。
- 团队与审计：团队隔离、角色管理、模型配置和操作记录。
- 内容规则集：设置页支持按条件、优先级、触发时机和范围自动执行规则；项目内置可复制的强规则预设见 [`rules/艺数智策-内容运营规则集.json`](rules/艺数智策-内容运营规则集.json)。预设不会自动覆盖已有团队规则，导入前可按团队负责人和平台名称调整。

本版本不含图片识别。所有导出通过已登录的应用请求下载，并依据响应头保存为对应的 PDF、CSV 或 Markdown 文件名。

## 模型接口设置

模型接口设置可配置 OpenAI 兼容服务商的地址、模型、鉴权方式、温度和失败降级。密钥由后端保存，浏览器不会直接请求模型服务。模型功能可留空，规则分析不要求 API Key。启用远程模型后，所选团队的相关运营摘要会发送到配置的服务商，请按其数据处理政策使用。

## 数据库迁移与自测

应用默认连接 PostgreSQL，启动时自动执行 Alembic 数据库迁移。若需手动查看或更新迁移，在 Windows 命令提示符运行：

```bat
.venv\Scripts\python.exe -m alembic current
.venv\Scripts\python.exe -m alembic upgrade head
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

本地 Python 环境会直接连接 `.env` 中的 PostgreSQL；如果没有 `.venv` 但检测到 Docker Compose，脚本会删除并重建 Compose 的 `postgres-data` 命名卷，再用本地已有镜像启动服务，不拉取镜像、不需要外网。Windows 可在 Git Bash 或 WSL 中执行；生产数据库请先备份再使用。

通过时会显示检查通过的总数。API 健康状态位于 `/api/health`，交互式 API 文档位于 `/docs`。

## 数据与安全

请定期备份 PostgreSQL，并将备份放在独立于应用目录的位置。团队网络部署应设置强随机 `APP_SECRET`、限制数据库和应用端口，并通过 HTTPS 反向代理访问。不要录入观众个人隐私信息，也不要将 `.env` 文件分享给他人。


## CSV 字段说明

内容导入模板提供现有业务字段：平台、发布时间、标题、内容类型、时长、播放/互动数据、看完比例、状态标签、标签、素材说明、责任人、备注、转化数据和评论摘录。必填字段为平台、发布时间、标题、播放量、点赞量；状态标签仅允许“已发布”或“未发布”，省略时按“已发布”导入。模板包含一条示例记录。

内容记录页支持关键词搜索、平台/类型/状态筛选、排序、分页和批量操作；查看者保留只读权限。排期日历默认打开当前月份，月历与月清单展示同一月份的计划。
