import { defineConfig, loadEnv } from "vite";

/**
 * Normalizes the proxy target taken from `VITE_DEV_API_TARGET`.
 *
 * Vite requires a real URL here: a bare `host:port` (which is how people copy
 * an address out of a browser, and how a backend address is often written in
 * `.env`) makes the dev server fail with an opaque error on the first `/api`
 * request. A missing scheme therefore becomes `http://`, mirroring
 * `src/lib/apiBase.ts`, and a trailing slash is dropped.
 */
export function resolveDevApiTarget(raw: string | undefined | null): string {
  const value = String(raw ?? "").trim().replace(/\/+$/, "");
  if (!value) {
    return "http://localhost:8080";
  }
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(value)) {
    return value;
  }
  return `http://${value}`;
}

function readGoDuration(raw: string | undefined, fallback: number): number {
  const value = raw?.trim();
  if (!value) return fallback;
  const token = /(\d+(?:\.\d+)?)(ms|s|m|h)/gy;
  let consumed = 0;
  let total = 0;
  let match: RegExpExecArray | null;
  while ((match = token.exec(value)) !== null) {
    consumed += match[0].length;
    const unit = match[2] === "h" ? 3_600_000 : match[2] === "m" ? 60_000 : match[2] === "s" ? 1_000 : 1;
    total += Number(match[1]) * unit;
  }
  return consumed === value.length && total > 0 ? total : fallback;
}

/**
 * The dev server keeps the browser on one origin and proxies `/api` to the Go
 * backend, so the frontend needs no CORS setup and no backend address in the
 * browser bundle.
 *
 * `VITE_DEV_API_TARGET` (web/.env) decides where that proxy points: the remote
 * deployment (`http://59.110.90.28:8080`), a local Go API
 * (`http://localhost:8080`, the default), or the Compose Web entrypoint
 * (`http://localhost`). Point the variable at another host instead of editing
 * this file.
 */
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, ".", "");
  const rootEnv = loadEnv(mode, "..", "");
  const devApiTarget = resolveDevApiTarget(process.env.VITE_DEV_API_TARGET || env.VITE_DEV_API_TARGET);
  const devPort = Number(process.env.WEB_DEV_PORT || env.WEB_DEV_PORT || 5173);
  const requestTimeoutMs = readGoDuration(process.env.REQUEST_TIMEOUT || env.REQUEST_TIMEOUT || rootEnv.REQUEST_TIMEOUT, 15_000);
  const aiRecommendationTimeoutMs = readGoDuration(process.env.AI_RECOMMENDATION_TIMEOUT || env.AI_RECOMMENDATION_TIMEOUT || rootEnv.AI_RECOMMENDATION_TIMEOUT, 30_000);
  const proxyTimeoutMs = Math.max(20_000, aiRecommendationTimeoutMs + 2 * requestTimeoutMs + 30_000);
  return {
    server: {
      host: "0.0.0.0",
      port: Number.isFinite(devPort) && devPort > 0 ? devPort : 5173,
      proxy: {
        "/api": {
          target: devApiTarget,
          changeOrigin: true,
          // Leave room for AI_RECOMMENDATION_TIMEOUT plus the API's request-data
          // and persistence windows. The API still enforces the model wait limit.
          proxyTimeout: proxyTimeoutMs,
          // The API answers `403 跨域来源未获允许` whenever a request carries an
          // `Origin` that is not listed in the server's CORS_ALLOWED_ORIGINS.
          // Browsers send `Origin` on POST/PATCH/DELETE even for same-origin
          // calls, so forwarding it would make every write fail (logging in,
          // creating the team administrator, saving records) while reads keep
          // working — the exact "only some buttons fail" symptom. The browser
          // only ever talks to the dev server, so this request is not
          // cross-origin: drop the header and let the proxy act as the client.
          configure: (proxy) => {
            proxy.on("proxyReq", (proxyReq) => {
              proxyReq.removeHeader("origin");
            });
          },
        },
      },
    },
    build: { outDir: "dist", emptyOutDir: true },
  };
});
