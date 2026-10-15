import { startupEvent, startupPhase, withStartupTrace } from "@neko/telemetry/startup";
import { randomUUID } from "node:crypto";
import { NextRequest } from "next/server";
import {
  enforceWorkflowApiEdgeThrottle,
  getWorkflowApiRunStatus,
  parseWorkflowApiBearer,
} from "@neko/llm/workflows";
import {
  workflowApiErrorResponse,
  workflowApiFingerprint,
  workflowApiJson,
} from "@/lib/workflow-api-http";
import { withSavedPublicUrl } from "@/lib/public-url-settings";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ workflowId: string; runId: string }>;
};

export async function GET(request: NextRequest, context: RouteContext) {
  const { runId } = await context.params;
  return withStartupTrace({ requestId: randomUUID(), workflowRunId: runId }, () => startupPhase("workflow.http_status", async () => {
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
    const run = await startupPhase("workflow.api_status", async () => getWorkflowApiRunStatus({
      workflowId,
      runId,
      token: token ?? "",
      clientFingerprint: fingerprint,
    }));
    return workflowApiJson(
      {
        ...run,
        createdAt: run.createdAt.toISOString(),
        admittedAt: run.admittedAt.toISOString(),
        startedAt: run.startedAt?.toISOString() ?? null,
        finishedAt: run.finishedAt?.toISOString() ?? null,
        expiresAt: run.expiresAt.toISOString(),
        artifact: run.artifact ? { ...run.artifact, url: await withSavedPublicUrl(run.artifact.url) } : null,
      },
      {
        headers: run.retryAfterSeconds
          ? { "Retry-After": String(run.retryAfterSeconds) }
          : undefined,
      },
    );
  } catch (error) {
    return workflowApiErrorResponse(error);
  }
}
