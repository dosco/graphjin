import { startupEvent, startupPhase, withStartupTrace } from "@neko/telemetry/startup";
import { randomUUID } from "node:crypto";
import { createReadStream } from "node:fs";
import { Readable } from "node:stream";
import { NextRequest, NextResponse } from "next/server";
import {
  enforceWorkflowApiEdgeThrottle,
  getWorkflowApiArtifact,
  parseWorkflowApiBearer,
} from "@neko/llm/workflows";
import {
  workflowApiErrorResponse,
  workflowApiFingerprint,
} from "@/lib/workflow-api-http";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ workflowId: string; runId: string }>;
};

export async function GET(request: NextRequest, context: RouteContext) {
  const { runId } = await context.params;
  return withStartupTrace({ requestId: randomUUID(), workflowRunId: runId }, () => startupPhase("workflow.http_artifact", async () => {
    const response = await getWorkflow(request, context);
    startupEvent("workflow.http_response", { statusCode: response.status });
    return response;
  }));
}

async function getWorkflow(request: NextRequest, context: RouteContext) {
  const fingerprint = workflowApiFingerprint(request);
  try {
    await startupPhase("workflow.api_throttle", async () => enforceWorkflowApiEdgeThrottle(fingerprint));
    const { workflowId, runId } = await context.params;
    const token = parseWorkflowApiBearer(request.headers.get("authorization"));
    const artifact = await startupPhase("workflow.api_artifact", async () => getWorkflowApiArtifact({
      workflowId,
      runId,
      token: token ?? "",
      clientFingerprint: fingerprint,
    }));
    const source = createReadStream(artifact.absolutePath);
    const started = performance.now();
    let outcome = "cancelled";
    source.once("end", () => { outcome = "completed"; });
    source.once("error", () => { outcome = "failed"; });
    source.once("close", () => startupEvent("workflow.artifact_stream", {
      outcome, durationMs: performance.now() - started, bytesRead: source.bytesRead,
    }));
    const body = Readable.toWeb(source);
    return new NextResponse(body as ReadableStream, {
      headers: {
        "Content-Type": artifact.contentType,
        "Content-Length": String(artifact.bytes),
        "Content-Disposition": `attachment; filename="${artifact.fileName}"`,
        "Cache-Control": "no-store, private",
        "X-Content-Type-Options": "nosniff",
      },
    });
  } catch (error) {
    return workflowApiErrorResponse(error);
  }
}
