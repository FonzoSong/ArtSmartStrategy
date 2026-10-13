import type { ApiErrorBody } from "../types/domain";
import { resolveAPIBaseURL } from "../lib/apiBase";
import { parseResponseBody } from "../lib/http";

const baseURL = resolveAPIBaseURL(import.meta.env.VITE_API_BASE_URL);

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
  // 204/205 (and any empty-body success) carry no JSON; decoding them as JSON
  // would throw and report a failed operation after a successful write.
  return parseResponseBody(response.status, await response.text()) as T;
}

export function resolveAPIURL(path: string): string {
  return `${baseURL}${path.startsWith("/") ? path : `/${path}`}`;
}
