import { NextRequest } from "next/server";
import {
  consumeReckonResultRate,
  getCompatWebhook,
  getReckonRun,
  reckonNotFound,
} from "@neko/llm/workflows/compat";
import {
  authorizeReckonToken,
  reckonErrorResponse,
  reckonJson,
  serveReckonRun,
} from "@/lib/reckon-webhook";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = { params: Promise<{ workflowId: string; runId: string }> };

export async function GET(request: NextRequest, context: RouteContext) {
  const { workflowId, runId } = await context.params;
  try {
    const webhook = await getCompatWebhook(workflowId);
    if (!webhook) throw reckonNotFound();
    authorizeReckonToken(webhook, request);
    await consumeReckonResultRate(webhook);

    const run = await getReckonRun(workflowId, runId);
    if (!run) throw reckonNotFound();
    if (run.expired) {
      return reckonJson({ error: "artifact_expired", status: run.status, runId }, 410);
    }
    if (run.status === "queued" || run.status === "running") {
      return reckonJson({ ok: true, status: run.status, runId, progress: run.progress }, 202, 10);
    }
    return serveReckonRun(run);
  } catch (error) {
    return reckonErrorResponse(error);
  }
}
