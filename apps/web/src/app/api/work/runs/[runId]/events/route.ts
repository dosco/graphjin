import { NextResponse } from "next/server";
import { getOrgId } from "@/lib/db";
import { getWorkRunEvents } from "@/lib/work-store";
import { getAuthorizedWorkRun } from "@/lib/work-thread-auth";

type RouteContext = {
  params: Promise<{ runId: string }>;
};

export async function GET(_: Request, context: RouteContext) {
  const { runId } = await context.params;
  const orgId = await getOrgId();
  const run = await getAuthorizedWorkRun(orgId, runId);
  if (!run) {
    return NextResponse.json({ error: "Run not found" }, { status: 404 });
  }
  const events = await getWorkRunEvents(orgId, runId);
  return NextResponse.json({ runId, events });
}
