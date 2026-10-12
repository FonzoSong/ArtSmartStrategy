import type { ApiErrorBody } from "../types/domain";

const baseURL = (import.meta.env.VITE_API_BASE_URL || "http://localhost:8080")
  .replace(/\/$/, "");

export class APIError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(message: string, status: number, code = "request_failed") {
    super(message);
    this.name = "APIError";
    this.status = status;
    this.code = code;
  }
}

export async function requestJSON<T>(
  path: string,
  options: RequestInit,
  headers: HeadersInit,
): Promise<T> {
  const response = await fetch(`${baseURL}${path}`, { ...options, headers });
  if (!response.ok) {
    let message = `请求失败 (${response.status})`;
    let code = "request_failed";
    try {
      const body = (await response.json()) as Partial<ApiErrorBody>;
      message = body.detail || body.error?.message || message;
      code = body.error?.code || code;
    } catch {
      // Keep the HTTP status message when the server does not return JSON.
    }
    throw new APIError(message, response.status, code);
  }
  return (await response.json()) as T;
}

export function resolveAPIURL(path: string): string {
  return `${baseURL}${path.startsWith("/") ? path : `/${path}`}`;
}
