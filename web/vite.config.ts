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
  const devApiTarget = resolveDevApiTarget(process.env.VITE_DEV_API_TARGET || env.VITE_DEV_API_TARGET);
  const devPort = Number(process.env.WEB_DEV_PORT || env.WEB_DEV_PORT || 5173);
  return {
    server: {
      host: "0.0.0.0",
      port: Number.isFinite(devPort) && devPort > 0 ? devPort : 5173,
      proxy: {
        "/api": {
          target: devApiTarget,
          changeOrigin: true,
          // The backend itself gives up after REQUEST_TIMEOUT (15s), so a little
          // headroom here keeps a silent network drop from hanging the page.
          proxyTimeout: 20000,
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
