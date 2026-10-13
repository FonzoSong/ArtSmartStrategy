/**
 * Backend origin resolution for the API client.
 *
 * `VITE_API_BASE_URL` is the only place the frontend learns where the Go API
 * lives, so every page shares one configuration. An empty value means
 * "same origin": the browser requests `/api/...` from the page's own origin and
 * the dev server or production Nginx proxies it to the API. A browser can never
 * resolve a Docker-internal service name (for example `api:8080`), so such a
 * bare host is normalized into a real URL instead of silently becoming a path
 * under the web root.
 */
export function resolveAPIBaseURL(raw: string | undefined | null): string {
  const value = String(raw ?? "").trim().replace(/\/+$/, "");
  if (!value) {
    return "";
  }
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(value)) {
    return value;
  }
  if (/^[^/]+\.[^/]+/.test(value) || /^[^/]+:\d+$/.test(value)) {
    return `http://${value}`;
  }
  return value.startsWith("/") ? value.replace(/\/+$/, "") : `/${value}`;
}
