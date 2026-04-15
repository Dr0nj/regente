/**
 * HTTP Job Executor — Phase 10
 *
 * Executes real HTTP requests for HTTP-type jobs.
 * Supports GET/POST/PUT/PATCH/DELETE with configurable headers and body.
 * Includes dry-run mode that logs the request without executing.
 */

import type { HttpConfig } from "@/lib/job-config";

export interface HttpJobResult {
  /** Whether this was a dry-run (no real request made) */
  dryRun: boolean;
  /** HTTP status code (0 if dry-run or network error) */
  statusCode: number;
  /** Status text from response */
  statusText: string;
  /** Response body (truncated to 4KB) */
  responseBody: string;
  /** Response headers as record */
  responseHeaders: Record<string, string>;
  /** Duration in ms */
  durationMs: number;
  /** Whether the request was considered successful (2xx) */
  ok: boolean;
  /** The request that was sent (for audit/logging) */
  request: {
    url: string;
    method: string;
    headers: Record<string, string>;
    bodyPreview?: string;
  };
}

const MAX_RESPONSE_BODY = 4096;

/**
 * Execute an HTTP job. If dryRun is true, returns a simulated result
 * without making any network call.
 */
export async function executeHttpJob(
  config: HttpConfig,
  dryRun: boolean,
  signal?: AbortSignal,
): Promise<HttpJobResult> {
  const { url, method, headers = {}, body } = config;

  const request = {
    url,
    method,
    headers: { ...headers },
    bodyPreview: body ? body.slice(0, 500) : undefined,
  };

  // ── Dry-run: simulate without network call ──
  if (dryRun) {
    return {
      dryRun: true,
      statusCode: 0,
      statusText: "DRY_RUN",
      responseBody: `[dry-run] Would ${method} ${url}`,
      responseHeaders: {},
      durationMs: 0,
      ok: true,
      request,
    };
  }

  // ── Real execution ──
  const start = performance.now();

  const init: RequestInit = {
    method,
    headers: Object.keys(headers).length > 0 ? headers : undefined,
    signal,
  };

  // Only attach body for methods that support it
  if (body && method !== "GET" && method !== "DELETE") {
    init.body = body;
    // Auto-set Content-Type if not specified
    if (!Object.keys(headers).some((k) => k.toLowerCase() === "content-type")) {
      (init.headers as Record<string, string>)["Content-Type"] = "application/json";
    }
  }

  const response = await fetch(url, init);
  const durationMs = Math.round(performance.now() - start);

  // Read response body (truncated)
  let responseBody: string;
  try {
    const text = await response.text();
    responseBody = text.length > MAX_RESPONSE_BODY
      ? text.slice(0, MAX_RESPONSE_BODY) + `\n... [truncated, ${text.length} bytes total]`
      : text;
  } catch {
    responseBody = "[could not read response body]";
  }

  // Collect response headers
  const responseHeaders: Record<string, string> = {};
  response.headers.forEach((value, key) => {
    responseHeaders[key] = value;
  });

  return {
    dryRun: false,
    statusCode: response.status,
    statusText: response.statusText,
    responseBody,
    responseHeaders,
    durationMs,
    ok: response.ok,
    request,
  };
}
