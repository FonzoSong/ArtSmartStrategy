"use strict";
/**
 * 艺数智策 · 前端自包含服务器（零第三方依赖：只用 Node 内置模块）
 *
 * 用途：把 web/out/ 里编译好的前端（HTML / CSS / JS / 图片）直接跑起来，
 *      目标机器上不需要 npm install、不需要 Docker、不需要 WSL。
 *
 * 启动：双击同目录的 start.bat，或手动执行
 *      node server.js [--port 8080] [--no-open]
 *
 * 行为：
 *   1. 端口从 8080 开始，被占用就自动顺延（8081、8082 …，最多 20 次）。
 *   2. 静态文件按扩展名给出正确 MIME；路径不带扩展名时回落到 index.html。
 *   3. 服务真正就绪后才打开系统默认浏览器，避免"浏览器先到、服务后到"的竞态。
 *   4. 可选的反向代理：配置后端地址后 /api 会被转发过去，浏览器始终只与本机
 *      同源通信（因此后端不需要 CORS）。地址来源优先级：
 *         环境变量 ARTSMART_API_TARGET > 同目录 api-target.txt
 *      未配置时 /api 直接返回 502 和一段可读提示，页面会提示"未配置后端 API"。
 *
 * 停止：在本窗口按 Ctrl+C。
 */

const http = require("node:http");
const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");

const ROOT = __dirname;
const HOST = process.env.ARTSMART_HOST || "127.0.0.1";
const DEFAULT_PORT = 8080;
const MAX_PORT_ATTEMPTS = 20;
const PROXY_TIMEOUT_MS = 20000;
const REQUEST_TIMEOUT_MS = readGoDuration(readProjectSetting("REQUEST_TIMEOUT"), 15000);
const AI_RECOMMENDATION_TIMEOUT_MS = readGoDuration(readProjectSetting("AI_RECOMMENDATION_TIMEOUT"), 30000);
const args = process.argv.slice(2);

/** 读取 `--flag value` 形式的命令行参数。 */
function readArg(name) {
  const index = args.indexOf(name);
  return index >= 0 && index + 1 < args.length ? args[index + 1] : "";
}

/** 起始端口：--port > ARTSMART_PORT > 8080。 */
function readStartPort() {
  const raw = readArg("--port") || process.env.ARTSMART_PORT || "";
  const port = Number.parseInt(raw, 10);
  return Number.isInteger(port) && port > 0 && port < 65536 ? port : DEFAULT_PORT;
}

function readGoDuration(value, fallback) {
  const input = String(value || "").trim();
  if (!input) return fallback;
  const token = /(\d+(?:\.\d+)?)(ms|s|m|h)/gy;
  let consumed = 0;
  let total = 0;
  let match;
  while ((match = token.exec(input)) !== null) {
    consumed += match[0].length;
    const unit = match[2] === "h" ? 3600000 : match[2] === "m" ? 60000 : match[2] === "s" ? 1000 : 1;
    total += Number(match[1]) * unit;
  }
  return consumed === input.length && total > 0 ? total : fallback;
}

function readProjectSetting(name) {
  if (process.env[name]) return process.env[name];
  const envFiles = [
    path.join(path.dirname(ROOT), ".env"),
    path.resolve(ROOT, "../../.env"),
  ];
  for (const filename of envFiles) {
    try {
      const line = fs.readFileSync(filename, "utf8").split(/\r?\n/).find((entry) => entry.trim().startsWith(`${name}=`));
      if (!line) continue;
      const value = line.slice(line.indexOf("=") + 1).trim();
      return value.replace(/^(['"])(.*)\1$/, "$2");
    } catch {
      // A standalone runtime is allowed to run without either optional env file.
    }
  }
  return "";
}

function proxyTimeoutMs(req) {
  const pathname = new URL(req.url || "/", "http://localhost").pathname;
  const isGeneration = req.method === "POST" && (
    pathname === "/api/ai/recommend" || /^\/api\/ai\/recommend\/[^/]+\/regenerate$/.test(pathname)
  );
  return isGeneration
    ? Math.max(PROXY_TIMEOUT_MS, AI_RECOMMENDATION_TIMEOUT_MS + 2 * REQUEST_TIMEOUT_MS + 30000)
    : PROXY_TIMEOUT_MS;
}

const OPEN_BROWSER = !args.includes("--no-open") && process.env.ARTSMART_OPEN !== "0";

const MIME_TYPES = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".map": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".jpg": "image/jpeg",
  ".jpeg": "image/jpeg",
  ".gif": "image/gif",
  ".webp": "image/webp",
  ".ico": "image/x-icon",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
  ".otf": "font/otf",
  ".txt": "text/plain; charset=utf-8",
  ".csv": "text/csv; charset=utf-8",
  ".pdf": "application/pdf",
  ".wasm": "application/wasm",
};

function log(message) {
  const now = new Date();
  const stamp = [now.getHours(), now.getMinutes(), now.getSeconds()]
    .map((value) => String(value).padStart(2, "0"))
    .join(":");
  console.log(`[${stamp}] ${message}`);
}

/** 把 `host:port` 之类的裸地址补全成带协议的 URL。 */
function normalizeTarget(value) {
  const trimmed = String(value || "").trim().replace(/\/+$/, "");
  if (!trimmed) return "";
  return /^[a-z][a-z0-9+.-]*:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`;
}

/** /api 的代理目标：环境变量优先，其次是同目录 api-target.txt（可含注释行）。 */
function resolveApiTarget() {
  const fromEnv = normalizeTarget(process.env.ARTSMART_API_TARGET);
  if (fromEnv) return fromEnv;
  try {
    const raw = fs.readFileSync(path.join(ROOT, "api-target.txt"), "utf8");
    const line = raw
      .split(/\r?\n/)
      .map((item) => item.trim())
      .find((item) => item && !item.startsWith("#"));
    if (line) return normalizeTarget(line);
  } catch {
    // 没有配置文件就是纯静态预览，属正常情况。
  }
  return "";
}

const API_TARGET = resolveApiTarget();

function sendJson(res, status, payload) {
  const body = Buffer.from(JSON.stringify(payload), "utf8");
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Content-Length": body.length,
    "Cache-Control": "no-store",
  });
  res.end(body);
}

function sendText(res, status, text) {
  const body = Buffer.from(text, "utf8");
  res.writeHead(status, {
    "Content-Type": "text/plain; charset=utf-8",
    "Content-Length": body.length,
    "Cache-Control": "no-store",
  });
  res.end(body);
}

/** 带哈希的构建产物可以长缓存，入口 HTML 必须每次校验。 */
function cacheControl(filePath) {
  return /[\\/]assets[\\/]/.test(filePath) ? "public, max-age=31536000, immutable" : "no-cache";
}

async function sendFile(res, filePath, status = 200) {
  const data = await fs.promises.readFile(filePath);
  res.writeHead(status, {
    "Content-Type": MIME_TYPES[path.extname(filePath).toLowerCase()] || "application/octet-stream",
    "Content-Length": data.length,
    "Cache-Control": cacheControl(filePath),
  });
  res.end(data);
}

/** 返回 HTTP 状态码，便于在控制台打印一行访问日志。 */
async function serveStatic(req, res, pathname) {
  let decoded = pathname;
  try {
    decoded = decodeURIComponent(pathname);
  } catch {
    sendText(res, 400, "请求路径编码无效");
    return 400;
  }
  // 目录穿越防护：解析后的路径必须仍在 out/ 之内。
  const resolved = path.resolve(ROOT, `.${decoded}`);
  if (resolved !== ROOT && !resolved.startsWith(ROOT + path.sep)) {
    sendText(res, 403, "拒绝访问该路径");
    return 403;
  }
  let target = decoded.endsWith("/") ? path.join(resolved, "index.html") : resolved;
  try {
    const stat = await fs.promises.stat(target);
    if (stat.isDirectory()) target = path.join(target, "index.html");
    await sendFile(res, target);
    return 200;
  } catch {
    // 找不到文件：无扩展名的路径回落到入口页（应用使用 #hash 路由）。
    if (!path.extname(decoded)) {
      try {
        await sendFile(res, path.join(ROOT, "index.html"));
        return 200;
      } catch {
        // 入口页也读不到时继续走下面的 404。
      }
    }
    sendJson(res, 404, { detail: `未找到 ${decoded}` });
    return 404;
  }
}

/**
 * 把 /api 转发给后端。
 *
 * 浏览器只访问本机的 127.0.0.1:<port>，所以这里必须删掉 Origin 头，与
 * vite.config.ts 里的开发代理保持一致：后端会拒绝不在 CORS_ALLOWED_ORIGINS
 * 名单里的来源，而浏览器的同源写请求同样会带上 Origin。
 */
function proxyApi(req, res) {
  const upstream = new URL(API_TARGET);
  const timeout = proxyTimeoutMs(req);
  const secure = upstream.protocol === "https:";
  const transport = secure ? require("node:https") : http;
  const headers = { ...req.headers, host: upstream.host };
  delete headers.origin;
  const client = transport.request(
    {
      hostname: upstream.hostname,
      port: upstream.port || (secure ? 443 : 80),
      method: req.method,
      path: req.url,
      headers,
      timeout,
    },
    (upstreamRes) => {
      log(`代理 ${req.method} ${req.url} → ${API_TARGET} · ${upstreamRes.statusCode}`);
      res.writeHead(upstreamRes.statusCode || 502, upstreamRes.headers);
      upstreamRes.pipe(res);
    },
  );
  client.on("timeout", () => client.destroy(new Error(`后端 ${timeout / 1000} 秒内没有响应`)));
  client.on("error", (error) => {
    const detail = `无法连接后端 ${API_TARGET}：${error.message}`;
    log(`代理失败 ${req.method} ${req.url}：${error.message}`);
    if (!res.headersSent) {
      sendJson(res, 502, { error: { code: "api_unreachable", message: detail }, detail });
    } else {
      res.end();
    }
  });
  req.pipe(client);
}

function handleRequest(req, res) {
  const requestURL = new URL(req.url || "/", `http://${req.headers.host || "localhost"}`);
  const pathname = requestURL.pathname;
  if (pathname === "/api" || pathname.startsWith("/api/")) {
    if (!API_TARGET) {
      log(`${req.method} ${pathname} → 502（未配置后端 API）`);
      sendJson(res, 502, {
        error: { code: "api_not_configured", message: "未配置后端 API" },
        detail:
          "未配置后端 API：请先启动后端服务，再把后端地址写进 out/api-target.txt（例如 http://localhost:8080），" +
          "或设置环境变量 ARTSMART_API_TARGET 后重新运行 start.bat。",
      });
      return;
    }
    proxyApi(req, res);
    return;
  }
  serveStatic(req, res, pathname)
    .then((status) => log(`${req.method} ${pathname} → ${status}`))
    .catch((error) => {
      log(`${req.method} ${pathname} → 500：${error.message}`);
      if (!res.headersSent) sendJson(res, 500, { detail: "服务器内部错误" });
    });
}

function openBrowser(url) {
  if (!OPEN_BROWSER) {
    log("已跳过自动打开浏览器（--no-open 或 ARTSMART_OPEN=0）");
    return;
  }
  const isWindows = process.platform === "win32";
  const command = isWindows ? "cmd" : process.platform === "darwin" ? "open" : "xdg-open";
  const commandArgs = isWindows ? ["/c", "start", "", url] : [url];
  try {
    spawn(command, commandArgs, { stdio: "ignore", detached: true, windowsHide: true }).unref();
    log(`已请求浏览器打开 ${url}（打不开时请手动复制到地址栏）`);
  } catch (error) {
    log(`自动打开浏览器失败（${error.message}），请手动访问 ${url}`);
  }
}

function printBanner(url) {
  console.log(
    [
      "",
      "============================================================",
      " 艺数智策 · 前端服务已启动",
      ` 页面地址：${url}`,
      ` 静态目录：${ROOT}`,
      ` 后端代理：${API_TARGET || "未配置（纯静态预览，/api 请求返回 502）"}`,
      " 停止服务：在本窗口按 Ctrl+C",
      "============================================================",
      "",
    ].join("\n"),
  );
}

function listen(port, attemptsLeft) {
  const server = http.createServer(handleRequest);
  server.on("error", (error) => {
    if (error.code === "EADDRINUSE" && !server.listening && attemptsLeft > 0) {
      log(`端口 ${port} 已被占用，改用 ${port + 1}`);
      listen(port + 1, attemptsLeft - 1);
      return;
    }
    console.error(`\n[错误] 服务启动失败：${error.message}`);
    process.exit(1);
  });
  server.listen(port, HOST, () => {
    const url = `http://localhost:${port}/`;
    printBanner(url);
    openBrowser(url);
  });
}

process.on("SIGINT", () => {
  console.log("\n已停止服务。");
  process.exit(0);
});

if (!fs.existsSync(path.join(ROOT, "index.html"))) {
  console.error(`\n[错误] ${ROOT} 下没有 index.html，请先运行 web/make.sh 重新构建 out/。`);
  process.exit(3);
}

listen(readStartPort(), MAX_PORT_ATTEMPTS);
