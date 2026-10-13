import { defineConfig, loadEnv } from "vite";

/**
 * The dev server keeps the browser on one origin and proxies `/api` to the Go
 * backend, so the frontend needs no CORS setup and no backend address in the
 * browser bundle.
 *
 * `VITE_DEV_API_TARGET` decides where that proxy points (default
 * `http://localhost:8080`, i.e. running the Go API locally). Point it at another
 * host — for example `http://localhost` when the API is only reachable through
 * the Compose Web entrypoint — instead of editing this file.
 */
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, ".", "");
  const devApiTarget = process.env.VITE_DEV_API_TARGET || env.VITE_DEV_API_TARGET || "http://localhost:8080";
  const devPort = Number(process.env.WEB_DEV_PORT || env.WEB_DEV_PORT || 5173);
  return {
    server: {
      host: "0.0.0.0",
      port: Number.isFinite(devPort) && devPort > 0 ? devPort : 5173,
      proxy: { "/api": { target: devApiTarget, changeOrigin: true } },
    },
    build: { outDir: "dist", emptyOutDir: true },
  };
});
