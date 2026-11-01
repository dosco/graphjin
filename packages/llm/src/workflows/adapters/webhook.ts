import type { ActionAdapter } from "../action-executor";

const DEFAULT_METHOD = "POST";
const DEFAULT_TIMEOUT_MS = 30_000;
const MAX_BODY_CAPTURE_BYTES = 4 * 1024;
const ALLOWED_METHODS = new Set([
  "GET",
  "POST",
  "PUT",
  "PATCH",
  "DELETE",
]);

export type WebhookPayload = {
  url?: string;
  method?: string;
  headers?: Record<string, string>;
  body?: string | Record<string, unknown> | unknown[];
  timeout_ms?: number;
};

export class WebhookAdapterError extends Error {
  constructor(
    message: string,
    public readonly status: number | null = null,
  ) {
    super(message);
    this.name = "WebhookAdapterError";
  }
}

/**
 * Generic outbound HTTP adapter. The host sets the request's `target` to
 * `payload.url` (see action-targets.ts), so rules match the URL this
 * adapter calls. The default `external_default` policy requires operator
 * approval for every URL; narrower org policies can use
 * `allowed_targets.patterns` to allow specific URLs (e.g. a corporate
 * Slack webhook) without approval.
 *
 * Captures status + first 4KB of response body + response headers into
 * the action_execution row. Non-2xx responses throw; the framework
 * marks the execution failed and the request status moves to failed.
 */
export const webhookAdapter: ActionAdapter = async ({ request }) => {
  const payload = (request.payload ?? {}) as WebhookPayload;
  const url = payload.url ?? request.target ?? null;
  if (!url) {
    throw new WebhookAdapterError(
      "webhook adapter requires a url (in payload.url or request.target)",
    );
  }
  const method = (payload.method ?? DEFAULT_METHOD).toUpperCase();
  if (!ALLOWED_METHODS.has(method)) {
    throw new WebhookAdapterError(
      `webhook adapter does not support method "${method}" (allowed: ${[
        ...ALLOWED_METHODS,
      ].join(", ")})`,
    );
  }

  const headers: Record<string, string> = {
    "User-Agent": "OpenNeko-webhook-adapter/1.0",
    ...(payload.headers ?? {}),
  };

  let body: string | undefined;
  if (payload.body !== undefined) {
    if (typeof payload.body === "string") {
      body = payload.body;
    } else {
      body = JSON.stringify(payload.body);
      if (!headers["Content-Type"] && !headers["content-type"]) {
        headers["Content-Type"] = "application/json";
      }
    }
  }

  const timeoutMs = payload.timeout_ms ?? DEFAULT_TIMEOUT_MS;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  let response: Response;
  try {
    response = await fetch(url, {
      method,
      headers,
      body,
      signal: controller.signal,
    });
  } catch (err) {
    if ((err as Error)?.name === "AbortError") {
      throw new WebhookAdapterError(
        `webhook to ${url} timed out after ${timeoutMs}ms`,
      );
    }
    throw new WebhookAdapterError(
      `webhook to ${url} failed before response: ${
        err instanceof Error ? err.message : String(err)
      }`,
    );
  } finally {
    clearTimeout(timer);
  }

  const rawBody = await response.text().catch(() => "");
  const capturedBody =
    rawBody.length > MAX_BODY_CAPTURE_BYTES
      ? rawBody.slice(0, MAX_BODY_CAPTURE_BYTES) +
        `\n…(+${rawBody.length - MAX_BODY_CAPTURE_BYTES} bytes truncated)`
      : rawBody;

  const responseHeaders: Record<string, string> = {};
  response.headers.forEach((value, key) => {
    responseHeaders[key] = value;
  });

  if (!response.ok) {
    throw new WebhookAdapterError(
      `webhook to ${url} returned ${response.status} ${response.statusText}: ${capturedBody.slice(0, 200)}`,
      response.status,
    );
  }

  return {
    commandOrOperation: `${method} ${url}`,
    externalRef: responseHeaders["x-request-id"] ?? null,
    result: {
      status: response.status,
      headers: responseHeaders,
      body: capturedBody,
    },
  };
};
