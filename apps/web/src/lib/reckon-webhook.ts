import { createReadStream } from "node:fs";
import { Readable } from "node:stream";
import { NextResponse } from "next/server";
import {
  getReckonRun,
  pickReckonArtifact,
  reckonFailureBody,
  reckonMissingBatchArtifactBody,
  reckonTokenMatches,
  ReckonWebhookError,
  RECKON_TOKEN_HEADER,
  type CompatWebhook,
  type ReckonRunRow,
} from "@neko/llm/workflows/compat";

export function reckonJson(
  body: Record<string, unknown>,
  status: number,
  retryAfterSeconds?: number,
  extraHeaders: Record<string, string> = {},
): NextResponse {
  return NextResponse.json(body, {
    status,
    headers: {
      ...(retryAfterSeconds ? { "Retry-After": String(retryAfterSeconds) } : {}),
      ...extraHeaders,
    },
  });
}

export function reckonErrorResponse(error: unknown): NextResponse {
  if (error instanceof ReckonWebhookError) {
    return reckonJson(error.body, error.status, error.retryAfterSeconds);
  }
  throw error;
}

export function authorizeReckonToken(webhook: CompatWebhook, request: Request): void {
  if (!reckonTokenMatches(request.headers.get(RECKON_TOKEN_HEADER) ?? "", webhook.tokenSha256)) {
    throw new ReckonWebhookError(401, { error: "unauthorized" });
  }
}

/** Reckon streams the result file itself, with no agent in the path. */
export async function serveReckonRun(run: ReckonRunRow): Promise<NextResponse> {
  if (run.status !== "completed") {
    const failure = reckonFailureBody(run);
    return reckonJson(failure.body, failure.status);
  }
  const artifact = await pickReckonArtifact(run);
  if (!artifact.ok) {
    if (artifact.reason === "missing_batch_result") {
      const failure = reckonMissingBatchArtifactBody(run);
      return reckonJson(failure.body, failure.status);
    }
    if (artifact.reason === "ambiguous") {
      return reckonJson(
        { error: "ambiguous_artifacts", status: run.status, runId: run.runId, files: artifact.names },
        409,
      );
    }
    return reckonJson({ error: "no_artifacts", status: run.status, runId: run.runId }, 404);
  }
  const body = Readable.toWeb(createReadStream(artifact.absolutePath)) as ReadableStream<Uint8Array>;
  return new NextResponse(body, {
    status: 200,
    headers: {
      "Content-Type": artifact.contentType,
      "Content-Length": String(artifact.bytes),
      "Content-Disposition": `${artifact.name.toLowerCase().endsWith(".csv") ? "attachment" : "inline"}; filename="${artifact.name.replace(/"/g, "")}"`,
      "Cache-Control": "no-store",
    },
  });
}

/** The POST reply for a finished run mirrors Reckon's completedRunResponse. */
export async function serveReckonPostResult(
  reckonWorkflowId: string,
  runId: string,
): Promise<NextResponse> {
  const run = await getReckonRun(reckonWorkflowId, runId);
  if (!run) return reckonJson({ error: "not found" }, 404);
  if (run.status !== "completed") {
    return reckonJson(
      { ok: false, runId: run.runId, status: run.status, error: run.error },
      run.status === "needs_input" ? 409 : 502,
    );
  }
  return serveReckonRun(run);
}
