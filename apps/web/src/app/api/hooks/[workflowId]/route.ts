import { NextRequest } from "next/server";
import {
  admitReckonWebhookRun,
  consumeReckonStartRate,
  filterTriggerParams,
  getCompatWebhook,
  getReckonRun,
  largestArrayLength,
  parseReckonIdempotencyKey,
  parseReckonMode,
  reckonNotFound,
  reckonResultUsage,
  RECKON_BODY_MAX_BYTES,
  RECKON_IDEMPOTENCY_HEADER,
  RECKON_MAX_BATCH_RECORDS,
  RECKON_SYNC_WAITERS,
  RECKON_WAIT_TIMEOUT_DEFAULT_MS,
  RECKON_WAIT_TIMEOUT_MAX_MS,
  ReckonWebhookError,
  type ReckonExecutionMode,
} from "@neko/llm/workflows/compat";
import {
  authorizeReckonToken,
  reckonErrorResponse,
  reckonJson,
  serveReckonPostResult,
} from "@/lib/reckon-webhook";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

declare global {
  var __reckonSyncWaiters: number | undefined;
}

type RouteContext = { params: Promise<{ workflowId: string }> };

function pendingResponse(input: {
  request: NextRequest;
  reckonWorkflowId: string;
  runId: string;
  mode: ReckonExecutionMode;
  status: "queued" | "running";
  replay: boolean;
}) {
  return reckonJson(
    {
      ok: true,
      runId: input.runId,
      status: input.status,
      mode: input.mode,
      idempotentReplay: input.replay,
      note: "Poll resultRequest.url with the same webhook token.",
      ...reckonResultUsage({
        baseUrl: input.request.nextUrl.origin,
        workflowId: input.reckonWorkflowId,
        runId: input.runId,
      }),
    },
    202,
    10,
    input.replay ? { "Idempotency-Replayed": "true" } : {},
  );
}

async function readBoundedBody(request: NextRequest): Promise<string> {
  const declared = Number(request.headers.get("content-length") ?? "0");
  if (Number.isFinite(declared) && declared > RECKON_BODY_MAX_BYTES) {
    throw new ReckonWebhookError(413, { error: "payload_too_large", maxBytes: RECKON_BODY_MAX_BYTES });
  }
  const text = await request.text();
  if (Buffer.byteLength(text, "utf8") > RECKON_BODY_MAX_BYTES) {
    throw new ReckonWebhookError(413, { error: "payload_too_large", maxBytes: RECKON_BODY_MAX_BYTES });
  }
  return text;
}

async function waitForTerminalRun(
  reckonWorkflowId: string,
  runId: string,
  timeoutMs: number,
): Promise<"terminal" | "queued" | "running"> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const run = await getReckonRun(reckonWorkflowId, runId);
    if (!run) return "queued";
    if (run.status !== "queued" && run.status !== "running") return "terminal";
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  const last = await getReckonRun(reckonWorkflowId, runId);
  return last?.status === "running" ? "running" : "queued";
}

export async function POST(request: NextRequest, context: RouteContext) {
  const { workflowId } = await context.params;
  try {
    const webhook = await getCompatWebhook(workflowId);
    if (!webhook) throw reckonNotFound();
    authorizeReckonToken(webhook, request);
    await consumeReckonStartRate(webhook);

    const idempotencyKey = parseReckonIdempotencyKey(request.headers.get(RECKON_IDEMPOTENCY_HEADER));
    if (!idempotencyKey) {
      throw new ReckonWebhookError(400, {
        error: "invalid_idempotency_key",
        message:
          "Send an Idempotency-Key header containing 1-200 letters, numbers, dots, underscores, colons, or dashes.",
      });
    }
    const mode = parseReckonMode(request.nextUrl.searchParams.get("mode"));
    if (!mode) {
      throw new ReckonWebhookError(400, { error: "invalid_mode", message: 'mode must be "single" or "batch"' });
    }

    const text = await readBoundedBody(request);
    let raw: unknown = {};
    try {
      raw = text ? JSON.parse(text) : {};
    } catch {
      throw new ReckonWebhookError(400, { error: "invalid_json" });
    }
    if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
      throw new ReckonWebhookError(400, {
        error: "invalid_payload",
        message: "Request JSON must be an object.",
      });
    }
    const params = filterTriggerParams(raw as Record<string, unknown>, webhook.params);
    if (mode === "batch") {
      const maxRecords = Math.min(RECKON_MAX_BATCH_RECORDS, webhook.batchChunkSize);
      if (largestArrayLength(params) > maxRecords) {
        throw new ReckonWebhookError(413, {
          error: "too_many_records",
          message: `Batch arrays may contain at most ${maxRecords} records.`,
          maxRecords,
        });
      }
    }

    const admitted = await admitReckonWebhookRun({
      webhook,
      mode,
      params,
      idempotencyKey,
      requestBytes: Buffer.byteLength(text, "utf8"),
    });

    if (admitted.replay) {
      const run = await getReckonRun(workflowId, admitted.runId);
      if (!run) return reckonJson({ error: "idempotency_run_missing" }, 409);
      if (run.status === "queued" || run.status === "running") {
        return pendingResponse({
          request,
          reckonWorkflowId: workflowId,
          runId: admitted.runId,
          mode: admitted.mode,
          status: run.status,
          replay: true,
        });
      }
      const response = await serveReckonPostResult(workflowId, admitted.runId);
      response.headers.set("Idempotency-Replayed", "true");
      return response;
    }

    const wants = request.nextUrl.searchParams.get("wait");
    const waiters = globalThis.__reckonSyncWaiters ?? 0;
    if ((wants !== "true" && wants !== "1") || !admitted.immediatelyEligible || waiters >= RECKON_SYNC_WAITERS) {
      return pendingResponse({
        request,
        reckonWorkflowId: workflowId,
        runId: admitted.runId,
        mode: admitted.mode,
        status: "queued",
        replay: false,
      });
    }

    const timeoutMs =
      Math.min(
        RECKON_WAIT_TIMEOUT_MAX_MS,
        Math.max(1_000, (Number(request.nextUrl.searchParams.get("wait_timeout")) || 0) * 1_000),
      ) || RECKON_WAIT_TIMEOUT_DEFAULT_MS;
    globalThis.__reckonSyncWaiters = waiters + 1;
    let outcome: "terminal" | "queued" | "running";
    try {
      outcome = await waitForTerminalRun(workflowId, admitted.runId, timeoutMs);
    } finally {
      globalThis.__reckonSyncWaiters = Math.max(0, (globalThis.__reckonSyncWaiters ?? 1) - 1);
    }
    if (outcome !== "terminal") {
      return pendingResponse({
        request,
        reckonWorkflowId: workflowId,
        runId: admitted.runId,
        mode: admitted.mode,
        status: outcome,
        replay: false,
      });
    }
    return serveReckonPostResult(workflowId, admitted.runId);
  } catch (error) {
    return reckonErrorResponse(error);
  }
}
