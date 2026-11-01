import { NextResponse } from "next/server";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { readRunArtifact } from "@/lib/work-files";
import { getWorkRunEvents } from "@/lib/work-store";
import { getAuthorizedWorkRun } from "@/lib/work-thread-auth";
import { isEmittedRunArtifact } from "@/lib/work-artifacts";

type RouteContext = {
  params: Promise<{ path: string[] }>;
};

export const runtime = "nodejs";

export async function GET(_: Request, context: RouteContext) {
  try {
    const { path } = await context.params;
    const relativePath = (path ?? []).join("/");
    const orgId = await getOrgId();
    const actor = await getCurrentActor();
    // This endpoint is a download boundary, not a workspace file browser.
    // Only an artifact event from an authorized run can grant access.
    const runId = path?.[0] === "runs" ? path[1] ?? "" : "";
    if (
      !runId ||
      path?.[2] !== "artifacts" ||
      path.length < 4 ||
      !(await getAuthorizedWorkRun(orgId, runId, actor))
    ) {
      return NextResponse.json({ error: "not found" }, { status: 404 });
    }
    const events = await getWorkRunEvents(orgId, runId);
    if (!isEmittedRunArtifact(events, runId, relativePath)) {
      return NextResponse.json({ error: "not found" }, { status: 404 });
    }
    const file = await readRunArtifact(orgId, runId, path.slice(3).join("/"));
    const body = new Uint8Array(file.data);
    const safeName = file.filename.replace(/"/g, "");
    return new NextResponse(body, {
      status: 200,
      headers: {
        "content-type": file.mimeType,
        "content-disposition": `attachment; filename="${safeName}"`,
        "x-content-type-options": "nosniff",
      },
    });
  } catch {
    // Do not turn path-validation or filesystem errors into a workspace
    // oracle. Every denied/missing download has the same response.
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
}
