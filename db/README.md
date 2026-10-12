# PostgreSQL 初始化

项目使用 PostgreSQL 16。Docker Compose 中数据库是独立服务，只连接内部
`db-net` 网络；数据库没有宿主机端口。应用用 `arts_app` 非超级用户账号访问它。
`arts_admin` 只用于 PostgreSQL 初始化，不会传给前端或 API。

## 首次启动

在仓库根目录复制 `.env.example` 到 `.env`，设置强随机的
`POSTGRES_ADMIN_PASSWORD`、`APP_DB_PASSWORD` 和至少 32 字符的 `APP_SECRET`，然后运行：

```text
docker compose config -q
docker compose up --build --detach --wait
docker compose ps
docker compose logs db api web
```

数据库镜像只在 named volume `arts-smart-postgres-data` 为空时执行
`init/01_schema.sql` 和 `init/02_seed_data.sh`。Schema 是当前 Go 迁移的 PostgreSQL
16 初始化副本，并将 `0001_initial` 写入 `schema_migrations`；API 启动时用 advisory
lock 检查并应用剩余 Go 迁移，成功后才通过 readiness 检查。

`LOAD_EXAMPLE_DATA=true`（默认）时，初始化脚本执行 `sql/02_seed_data.sql`，插入 120
条虚构内容、演出、Tag、排期、统计基准和只读规则集。示例数据不创建账号；首次访问 Web
页面后创建团队管理员。六类工作流状态均有记录，发布指标只出现在已发布记录上。

初始化脚本不会在已有数据库卷上再次运行。更改 SQL 后，已有卷不会自动更新；Schema 升级
由 Go 的版本化迁移管理。不要为重新导入样例数据运行 `docker compose down -v`，这会删除
数据库持久化卷中的全部数据。

## 手动建库和初始化

正常 Docker 启动由官方 PostgreSQL 镜像根据 `POSTGRES_DB` 建库，并自动执行初始化目录。
`sql/create_database.sql` 只用于手工创建数据库：设置 `POSTGRES_DB` 后，连接维护库执行：

```text
psql -U arts_admin -d postgres -v ON_ERROR_STOP=1 -f db/sql/create_database.sql
```

不要在 Docker 已自动创建数据库后再次执行建库脚本。若需要手工装载 schema，应使用官方
PostgreSQL `psql`，并确保 `APP_DB_USER`、`APP_DB_PASSWORD` 环境变量已设置：

```text
psql -U arts_admin -d arts_smart -v ON_ERROR_STOP=1 -f db/init/01_schema.sql
psql -U arts_admin -d arts_smart -v ON_ERROR_STOP=1 -f db/sql/02_seed_data.sql
```

`01_schema.sql` 使用 psql 的 `\\getenv`/`\\gexec` 指令从环境创建最小权限应用角色，
再以该角色创建业务对象；它不是通用 SQL 客户端可直接执行的纯 SQL 文件。`02_seed_data.sql`
可重复执行，唯一键冲突会被忽略，内容样例仅在团队尚无内容时生成。

## 历史数据迁移

当前工作区原有 SQLite 历史数据已迁入 `arts-smart-postgres-data` 命名卷，并核对了源端与
目标端各业务表的行数。源 SQLite 文件和一次性导入工具已清理；运行中的 API 不读取本地
业务文件。新的环境请使用 PostgreSQL Schema 和可选示例 SQL 初始化，不要尝试重复导入旧
SQLite 数据。

## 常见问题

- `database is starting`：等待 `docker compose ps` 中 `db` 为 healthy；查看 `docker compose logs db`。
- `APP_DB_PASSWORD`/`POSTGRES_ADMIN_PASSWORD` 缺失：在 `.env` 配置 URL 安全字符集的随机值；
  PostgreSQL 镜像只在空数据卷初始化角色。更改现有实例的 `APP_DB_PASSWORD` 后，Compose
  环境变量不会自动改写数据库内的角色密码。请先更新 `.env`，再执行
  `docker compose exec db psql -U arts_admin -d arts_smart`，在 psql 中运行 `\\password arts_app`
  并输入与 `.env` 完全相同的新值，然后运行 `docker compose up -d api`。这不会改动业务数据
  或数据库卷。
- API readiness 失败：查看 `docker compose logs api`，确认数据库健康、secret 长度和迁移状态。
- 初始化脚本改动未生效：官方镜像只在空数据目录运行初始化脚本；先备份并在隔离环境验证，
  使用数据库迁移管理已有数据，不要直接删除命名卷。
- Web 页面无法加载 API：Web 通过 Nginx 同源 `/api/` 反向代理到 `api:8080`；检查两者健康
  状态和 `app-net`，不要将数据库地址暴露给浏览器。
