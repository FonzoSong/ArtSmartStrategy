# web 前端说明

艺数智策的浏览器端应用。代码位于 `web/`，与仓库根目录的 Go API 完全分离：前端通过
HTTP JSON 访问后端，不做任何数据库访问。

## 1. 技术栈（以 `package.json` 与 `tsconfig.json` 为准）

| 项目 | 实际情况 |
| --- | --- |
| 语言 | TypeScript `^5.7.3`（`strict: true`，`target: ES2022`，`module: ESNext`，`moduleResolution: Bundler`） |
| 运行时 | 浏览器原生 ES Module（无 React/Vue 等框架） |
| 构建工具 | Vite `^6.0.7`（`vite build`，输出 `dist/`） |
| 开发服务器 | `vite --host 0.0.0.0`（默认 5173 端口，内置 `/api` 代理） |
| 包管理器 | npm（存在 `package-lock.json`，Docker 中使用 `npm ci`） |
| 单元测试 | Node 内置 `node --test` 运行 `tests/*.test.ts`（`npm test`） |
| 类型检查 | `tsc --noEmit`（`npm run typecheck`） |

`package.json` 脚本：

```text
npm run dev        # 启动开发服务器（含 /api 代理）
npm test           # node --test，运行 tests/ 下的单元测试
npm run typecheck  # tsc --noEmit
npm run build      # vite build -> dist/
```

## 2. 目录结构

```text
web/
├── index.html                 # 唯一 HTML 入口：应用外壳、侧边导航、#pageRoot 挂载点
├── src/
│   ├── app.ts                 # 应用主文件：状态、路由分发、所有页面渲染与交互（含 @ts-nocheck）
│   ├── api/client.ts          # fetch 封装：附带会话 Token、统一错误体、204 处理
│   ├── components/workflow.ts # 工作流状态常量与配色
│   ├── lib/
│   │   ├── apiBase.ts         # 解析 VITE_API_BASE_URL，把裸主机名规范成 URL
│   │   ├── contracts.ts       # 各接口响应的归一化（缺字段不会变成 undefined）
│   │   ├── dates.ts           # 纯日历日期工具（YYYY-MM-DD，不做时区换算）
│   │   ├── format.ts          # 计数/百分比/分值格式化（统一两位小数）
│   │   ├── http.ts            # 响应体解码（204/空响应不抛错）
│   │   └── suggestions.ts     # 排期建议的可采纳规则与空状态文案
│   ├── pages/routes.ts        # 页面 key → 中文标题（同时作为路由白名单）
│   ├── styles.css             # 全量样式（宣纸暖色、水墨字色、青绿点缀、响应式）
│   └── types/domain.ts        # 共享领域类型（角色、生命周期、工作流、错误体）
├── public/                    # 原样拷入 dist/ 的静态资源
│   └── art-home-ink-ridge.svg # 首页青绿山水插画（本地可维护的原创 SVG）
├── tests/                     # node --test 单元测试（纯函数）
├── vite.config.ts             # 开发服务器与 /api 代理配置
├── tsconfig.json
├── .env / .env.example        # 前端环境变量（见第 4 节）
├── start.bat                  # Windows 非 Docker 一键启动脚本（见第 6 节）
├── Dockerfile / nginx.conf    # 生产镜像：构建静态文件并由 Nginx 托管
└── nginx.external.conf.template # 三服务器部署时的 Nginx 模板（API_UPSTREAM 可配）
```

## 3. 路由、页面与状态

- **路由体系**：hash 路由。`web/src/pages/routes.ts` 的 `pageLabels` 既是页面标题表，也是
  路由白名单（`Object.hasOwn(pages, page)`）。合法页面：

  | hash | 页面 |
  | --- | --- |
  | `#home` | 首页（产品首页，含品牌介绍、两项真实入口、四指标区、青绿山水插画） |
  | `#overview` | 运营总览 |
  | `#records` | 内容记录 |
  | `#performances` | 正式演出（列表 + 详情：预热聚合、时间轴、整场排期） |
  | `#analysis` | 效果分析 |
  | `#suggestions` | 排期建议 |
  | `#calendar` | 排期日历 |
  | `#settings` | 模型接口设置 |
  | `#team` | 艺术团队管理 |
  | `#rules` | 内容规则集 |
  | `#tags` | Tag 库 |

  未识别的 hash 与空 hash 都回落到 `#home`。切换页面用 `go(page)`：更新 hash、刷新
  导航激活态与面包屑，然后重新渲染。
- **状态管理**：没有第三方状态库。`app.ts` 顶部的单个 `state` 对象保存当前用户、当前
  页面、筛选条件、缓存的列表数据等；`render()` 会先调用 `refresh()` 重新拉取
  `contents / performances / users / tags / stats/overview / settings`，再渲染当前页面。
  页面内的选择（分页、筛选、模式）通过 `state` 字段保持。
- **渲染方式**：所有页面返回 HTML 字符串，写入 `#pageRoot`，随后 `wirePage()` 绑定事件
  （导航、`data-action`、`data-goto`、表单提交、日历拖拽等）。交互统一走 `data-action`，
  由 `action()` / `runExtraAction()` / `performanceAction()` / `rulesetAction()` 分发。
- **统一弹窗**：表单与确认都使用 `confirmDialog()`（`app.ts`），它负责字段渲染、加载态、
  就地错误提示、Esc 关闭与焦点返回；不要使用浏览器原生 `confirm/prompt`。
- **数据与展示分离**：接口响应先经过 `lib/contracts.ts` 的 `normalizeAPIResponse`，缺失
  字段会被补成安全的默认值；日期经 `lib/dates.ts` 归一为 `YYYY-MM-DD`；百分比统一走
  `lib/format.ts`（两位小数，空值显示 `—`）。

## 4. 与后端的通信

- **API 客户端**：`web/src/api/client.ts`
  - `requestJSON(path, options, headers)`：`fetch` 包装；非 2xx 时解析 `detail` /
    `error.message` 抛出 `APIError`；`204/205` 或空响应体返回 `null`（避免删除成功却报错）。
  - `resolveAPIURL(path)`：把相对路径拼成完整地址，供文件下载使用。
  - `baseURL` 来自 `resolveAPIBaseURL(import.meta.env.VITE_API_BASE_URL)`（见下）。
- **认证**：登录成功后把 Bearer token 存进 `sessionStorage.artsSessionToken`，每次请求带
  `Authorization: Bearer <token>`；`X-Team-Id` 用于团队上下文。权限最终由后端校验，前端
  隐藏按钮只是体验优化。
- **同源优先**：默认 `VITE_API_BASE_URL` 为空，浏览器请求同源 `/api/...`；开发时由 Vite
  代理转发，生产时由 Nginx 代理转发。

### 环境变量（`web/.env` 与 `web/.env.example`）

| 变量 | 用途 | 示例值 | 是否必填 | 改动后 |
| --- | --- | --- | --- | --- |
| `VITE_API_BASE_URL` | 浏览器访问后端的地址；留空表示同源 `/api` | 空（推荐）；或 `http://localhost:8080`；或 `https://arts.example.com` | 否（留空即可） | 需要重启开发服务器（Vite 只在启动时读取 env） |
| `VITE_DEV_API_TARGET` | 仅开发环境：Vite 的 `/api` 代理目标（后端地址写在这里）。裸 `host:port` 会被自动补成 `http://host:port` | `http://59.110.90.28:8080`（本仓库默认，后端在远端）；本机后端填 `http://localhost:8080`；Compose 里只有 API 时填 `http://localhost` | 否 | 需要重启开发服务器 |

说明：

- Vite 只暴露以 `VITE_` 开头的变量，所以变量名必须使用 `VITE_` 前缀。
- **不要把数据库地址、数据库账号、数据库口令、`APP_SECRET`、模型密钥写进 `web/.env`**：
  这些值会被编译进浏览器可见的产物。数据库凭据与模型密钥只存在于后端环境。
- 使用 Docker 部署时不要填 `api:8080`、`backend` 之类的容器内部服务名——浏览器无法解析它们。
  容器部署请保持留空（同源），由 Web 容器里的 Nginx 反代到 `api:8080`。
- 如果确实要跨域直连后端（例如后端在另一台主机），填写完整来源（含协议与端口），并在后端
  `CORS_ALLOWED_ORIGINS` 中登记前端来源。`lib/apiBase.ts` 会把裸 `host:port` 规范化成
  `http://host:port`，让配置错误立刻可见，而不是变成相对路径。
- **远端后端目前未开放任何浏览器跨域来源**：`http://59.110.90.28:8080` 对带 `Origin` 的
  请求一律返回 `403 跨域来源未获允许`（`origin_forbidden`），`http://localhost:5173` 也不例外。
  所以指向该后端时必须留空 `VITE_API_BASE_URL`，由开发服务器（或生产 Nginx）代理 `/api`；
  后端地址只写在 `VITE_DEV_API_TARGET`。
- **开发服务器会剥离 `Origin` 头**（`vite.config.ts` 的 `configure` → `proxyReq.removeHeader`）：
  浏览器对 POST/PATCH/DELETE **即使同源也会带 `Origin`**，原样转发就会被后端 CORS 中间件
  拦下——表现为「页面能打开、只读页面正常，但登录、创建团队管理员、保存内容时报
  403 跨域来源未获允许」。浏览器与开发服务器之间不是跨域请求，代理转发时去掉该头即可，
  不需要改后端。改完 `vite.config.ts` 必须重启开发服务器。
- 若要在生产环境跨来源直连后端（或由 Nginx 代理且浏览器来源不是后端白名单里的那个），
  正确做法是在后端 `CORS_ALLOWED_ORIGINS` 登记前端来源，而不是靠代理剥离请求头。

### 本地运行与 Docker 的差异

| 场景 | `VITE_API_BASE_URL` | `/api` 如何到达后端 |
| --- | --- | --- |
| 本机开发（后端在远端，本仓库默认） | 留空 | Vite 代理：`/api` → `VITE_DEV_API_TARGET`（`http://59.110.90.28:8080`） |
| 本机开发（非 Docker，本机跑 Go API） | 留空 | Vite 代理：`/api` → `VITE_DEV_API_TARGET`（默认 `http://localhost:8080`） |
| 本机开发（API 只在 Compose 里） | 留空 | 把 `VITE_DEV_API_TARGET` 设为 `http://localhost`（Compose Web 入口的 Nginx 会继续转发给 API） |
| 本机开发（直连后端） | `http://localhost:8080` | 浏览器直连；后端 `CORS_ALLOWED_ORIGINS` 需包含 `http://localhost:5173` |
| Docker Compose | 留空（构建参数传入） | 同源 Nginx：`location /api/` → `http://api:8080` |
| 三服务器部署 | 留空 | Web 容器 Nginx 使用 `API_UPSTREAM` 反代 API 服务器 |

## 5. Windows 非 Docker 启动

### 方式一：双击 `start.bat`（推荐）

双击 `web/start.bat`，或项目根目录的 `start.bat`（它直接调用前者）。两者都不需要 Docker。
见第 6 节，脚本会检查环境、启动开发服务器、等待就绪并打开浏览器。

### 方式二：手动执行

前提：Node.js 20+（含 npm），并确保 Go 后端已在监听（默认 `http://localhost:8080`）。

```text
cd web
npm ci                 # 或 npm install：按 package-lock.json 安装依赖
copy .env.example .env # 首次；若 .env 已存在则跳过，不要覆盖已有配置
npm run dev            # 启动开发服务器（默认 http://localhost:5173）
```

如果后端不是跑在本机 8080（例如只在 Docker Compose 中），在 `web/.env` 中设置
`VITE_DEV_API_TARGET=http://localhost` 后再启动；该变量只影响开发服务器的代理目标。

在另一个终端启动后端（参考仓库根目录 `README.md`），需要可访问的 PostgreSQL 与
`APP_SECRET`；本机开发时把 `CORS_ALLOWED_ORIGINS` 设为 `http://localhost:5173`，或完全依赖
Vite 的 `/api` 代理（此时浏览器只与 5173 通信，不触发跨域）。

### 修改后端地址

后端地址只写在 `web/.env` 的 `VITE_DEV_API_TARGET`（本仓库默认
`http://59.110.90.28:8080`），`VITE_API_BASE_URL` 保持留空表示同源访问。改完必须**重启**
`npm run dev`（或重新运行 `start.bat`）：Vite 只在启动时读取 env，只改文件不重启不会生效。
值可以是完整 URL，也可以只写 `host:port`——缺协议时 `vite.config.ts` 会补上 `http://`，
避免代理在第一个 `/api` 请求上抛出难懂的错误。

### 类型检查与生产构建

```text
npm run typecheck   # tsc --noEmit
npm test            # node --test
npm run build       # 产出 web/dist/
```

### 前后端连通性验证

```text
curl http://59.110.90.28:8080/api/health/live  # 后端存活（远端）
curl http://59.110.90.28:8080/api/health/ready # 数据库连通（远端）
curl http://localhost:5173/                    # 前端入口
curl http://localhost:5173/api/health/ready    # 经 Vite 代理访问后端（应与上一条一致）
```

打开页面后按 F12，Network 中应看到 `/api/...` 请求返回 200；Console 不应出现未捕获异常。
未登录时 `/api/contents` 等接口返回后端自己的 `401 {"detail":"请先登录"}`，这也是代理正常
的证据——说明响应来自后端而不是代理错误页。

## 6. `start.bat` 启动流程

`web/start.bat` 面向 Windows 非 Docker 环境，双击即可使用：

1. 切换到脚本所在目录（不依赖当前工作目录）并设置控制台编码为 UTF-8。
2. 检查 `node` 与 `npm` 是否存在。
3. 若 `node_modules` 不存在则执行一次 `npm ci`（存在则跳过，不会无条件重装）。
4. 若 `.env` 不存在，从 `.env.example` 复制一份（已存在则绝不覆盖）。
5. 读取 `.env` 并打印实际生效的后端地址：`VITE_DEV_API_TARGET` 是 `/api` 的代理目标；
   若 `VITE_API_BASE_URL` 非空则提示跨域前提（后端必须允许该来源）；随后用一次最长 3 秒的
   TCP 探测报告后端是否可达——不可达只提示，不阻止启动。
6. 使用 `npm run dev` 启动前端，日志留在当前窗口。
7. 轮询等待前端真正就绪（最多约 90 秒）后，用默认浏览器打开实际端口地址。
8. 失败时给出明确原因（缺依赖、端口被占用、准备超时）并返回非零退出码。

边界：它**不**启动或停止 Go 后端，**不**初始化或迁移数据库，**不**依赖 Docker/Compose，
也**不**会删除数据库数据。端口可通过 `WEB_DEV_PORT` 指定（默认 5173）。

## 7. 常见启动故障排查

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| `node 不是内部或外部命令` | 未安装 Node.js 或未加入 PATH | 安装 Node.js 20+ 并重新打开命令行 |
| `vite` 启动失败、提示端口被占用 | 5173 已被其他进程占用 | 结束占用进程，或设置 `WEB_DEV_PORT=5174` 后重新运行 `start.bat` |
| 页面能打开但接口全 404/红色错误 | 后端未启动，或 `VITE_API_BASE_URL`/`VITE_DEV_API_TARGET` 指向了错误地址 | 先 `curl http://59.110.90.28:8080/api/health/live`（或后端实际地址）；确认 `web/.env` 的 `VITE_API_BASE_URL` 留空、`VITE_DEV_API_TARGET` 指向正确主机，改后重启 dev server |
| 开发时 `/api/...` 返回 500 | Vite 代理目标不可达（默认 `http://localhost:8080`，但后端只在容器里） | 在 `web/.env` 设 `VITE_DEV_API_TARGET=http://localhost`（或后端实际地址）后重启 dev server |
| 浏览器报跨域错误，接口返回 `403 跨域来源未获允许` | 填写了完整的 `VITE_API_BASE_URL`，但后端未允许该来源（远端后端默认拒绝所有来源） | 把 `web/.env` 的 `VITE_API_BASE_URL` 留空，改用 `VITE_DEV_API_TARGET` 指向后端后重启 dev server；或在后端 `CORS_ALLOWED_ORIGINS` 登记前端来源 |
| 启动窗口提示"后端 当前无法连接" | 本机网络/防火墙/VPN 无法到达 `VITE_DEV_API_TARGET` | 页面仍可使用，但接口会失败；确认后端已启动、地址与端口正确，能 `curl` 通后重新运行 `start.bat` |
| **只读页面正常，但登录 / 创建团队管理员 / 保存时报 `403 跨域来源未获允许`** | 后端按 `Origin` 头做 CORS 白名单校验，而浏览器对写请求（POST/PATCH/DELETE）即使同源也会带 `Origin` | 开发服务器已默认剥掉该头（`vite.config.ts`），重启开发服务器即可；生产部署请把前端来源登记到后端 `CORS_ALLOWED_ORIGINS` |
| 依赖安装失败 | 网络或缓存问题 | 删除 `node_modules` 后重新 `npm ci`；企业网络需配置 npm registry 代理 |
| 修改 `.env` 没有效果 | Vite 只在启动时读取环境变量 | 重启 `npm run dev` |
| 登录后立刻 401 | 会话已过期或被吊销 | 重新登录；确认后端 `APP_SECRET` 未更换 |
| `dist/` 产物与源码不一致 | 忘记重新构建 | 运行 `npm run build`；Docker 部署则重新 `docker compose build web` |

## 8. 生产构建与部署

- `web/Dockerfile`：`node:22-alpine` 构建（`npm ci` → `typecheck` → `build`），再把 `dist/`
  交给 `nginx:1.27-alpine`；构建参数 `VITE_API_BASE_URL` 默认空串。
- `web/nginx.conf`：`try_files` 支持 hash 路由刷新；`location /api/` 反代到 `api:8080`。
- `web/nginx.external.conf.template`：三服务器部署模板，通过 `API_UPSTREAM` 指定 API 地址。
- 生产环境不暴露源码，`dist/` 内含哈希文件名的 JS/CSS；静态资源（如首页插画）位于
  `web/public/`，构建后可通过 `/art-home-ink-ridge.svg` 访问。

## 9. 开发约定

- 新增页面：先在 `src/pages/routes.ts` 注册 key，再在 `app.ts` 的页面映射与 `render()` 中
  接入，并在 `index.html` 的导航中指向真实页面。不要新增无功能的占位入口。
- 新增接口：在 `lib/contracts.ts` 增加归一化并在 `normalizeAPIResponse` 注册，避免页面直接
  读取可能缺失的字段。
- 数字与日期：一律经 `lib/format.ts` 与 `lib/dates.ts`，不要在各处手写 `toFixed` 或
  `slice`。
- 删除等 204 接口：直接使用 `api(path, { method: "DELETE" })`，客户端已处理空响应体。
- 视觉：沿用 `styles.css` 的 CSS 变量（`--paper`、`--ink`、`--line`、`--blue` 等）与既有
  组件类，保持宣纸暖色、细线分隔、克制动效。
