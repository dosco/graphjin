import { NextResponse } from "next/server";
import {
  WORKFLOW_API_HARD_MAX_REQUEST_BYTES,
  WorkflowApiError,
  workflowApiClientFingerprint,
} from "@neko/llm/workflows";

export function workflowApiFingerprint(request: Request): string {
  const forwarded = request.headers.get("x-forwarded-for")?.split(",")[0]?.trim();
  const address = forwarded || request.headers.get("x-real-ip") || "unknown";
  return workflowApiClientFingerprint(address);
}

/** Read a public API body incrementally so chunked uploads cannot bypass the cap. */
export async function readWorkflowApiJsonBody(
  request: Request,
  maxBytes = WORKFLOW_API_HARD_MAX_REQUEST_BYTES,
): Promise<unknown> {
  const declaredLength = Number.parseInt(
    request.headers.get("content-length") ?? "0",
    10,
  );
  if (Number.isFinite(declaredLength) && declaredLength > maxBytes) {
    throw new WorkflowApiError(
      "request_too_large",
      "The request exceeds the deployment safety limit.",
      413,
    );
  }

  const reader = request.body?.getReader();
  const chunks: Uint8Array[] = [];
  let bytes = 0;
  if (reader) {
    while (true) {
      const next = await reader.read();
      if (next.done) break;
      bytes += next.value.byteLength;
      if (bytes > maxBytes) {
        await reader.cancel().catch(() => undefined);
        throw new WorkflowApiError(
          "request_too_large",
          "The request exceeds the deployment safety limit.",
          413,
        );
      }
      chunks.push(next.value);
    }
  }

  const raw = Buffer.concat(chunks.map((chunk) => Buffer.from(chunk))).toString(
    "utf8",
  );
  try {
    return JSON.parse(raw);
  } catch {
    throw new WorkflowApiError(
      "invalid_json",
      "The request body is not valid JSON.",
      400,
    );
  }
}

export function workflowApiJson(
  body: unknown,
  init: ResponseInit = {},
): NextResponse {
  const headers = new Headers(init.headers);
  headers.set("Cache-Control", "no-store, private");
  headers.set("X-Content-Type-Options", "nosniff");
  return NextResponse.json(body, { ...init, headers });
}

export function workflowApiErrorResponse(error: unknown): NextResponse {
  if (error instanceof WorkflowApiError) {
    const headers = new Headers();
    if (error.retryAfterSeconds !== undefined) {
      headers.set("Retry-After", String(error.retryAfterSeconds));
    }
    if (error.status === 401) {
      headers.set("WWW-Authenticate", 'Bearer realm="workflow-api"');
    }
    return workflowApiJson(
      { error: { code: error.code, message: error.message } },
      { status: error.status, headers },
    );
  }
  console.error(
    `[workflow-api] request failed: ${error instanceof Error ? error.message : String(error)}`,
  );
  return workflowApiJson(
    {
      error: {
        code: "internal_error",
        message: "The workflow API request could not be completed.",
      },
    },
    { status: 500 },
  );
}
