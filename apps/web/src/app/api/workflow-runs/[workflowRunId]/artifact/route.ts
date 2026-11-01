import { createReadStream } from "node:fs";
import { Readable } from "node:stream";
import { NextResponse } from "next/server";
import {
  WorkflowApiError,
  getWorkflowApiArtifactForOperator,
} from "@neko/llm/workflows";
import { getOrgId } from "@/lib/db";
import { requireWorkflowRun } from "@/lib/entitlements";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ workflowRunId: string }>;
};

export async function GET(_request: Request, context: RouteContext) {
  const { workflowRunId } = await context.params;
  const deniedRun = await requireWorkflowRun(workflowRunId);
  if (deniedRun) return deniedRun;
  const orgId = await getOrgId();
  try {
    const artifact = await getWorkflowApiArtifactForOperator({
      orgId,
      runId: workflowRunId,
    });
    const body = Readable.toWeb(createReadStream(artifact.absolutePath));
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
    if (error instanceof WorkflowApiError) {
      return NextResponse.json(
        { error: { code: error.code, message: error.message } },
        {
          status: error.status,
          headers: { "Cache-Control": "no-store, private" },
        },
      );
    }
    console.error(
      `[workflow-api-artifact] operator download failed: ${error instanceof Error ? error.message : String(error)}`,
    );
    return NextResponse.json(
      { error: { code: "internal_error", message: "Artifact download failed." } },
      { status: 500 },
    );
  }
}
