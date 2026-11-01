import { NextRequest, NextResponse } from "next/server";
import { db, eq, work_message } from "@neko/db";
import { getOrgId } from "@/lib/db";
import { truncateWorkThreadFromRun } from "@/lib/work-store";
import { getAuthorizedWorkThread } from "@/lib/work-thread-auth";

type RouteContext = {
  params: Promise<{ threadId: string }>;
};

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(request: NextRequest, context: RouteContext) {
  const { threadId } = await context.params;
  const body = await request.json().catch(() => ({}));
  const messageId = typeof body.messageId === "string" ? body.messageId.trim() : "";
  if (!messageId) {
    return NextResponse.json({ error: "messageId is required" }, { status: 400 });
  }

  const orgId = await getOrgId();
  const thread = await getAuthorizedWorkThread(orgId, threadId);
  if (!thread) {
    return NextResponse.json({ error: "Thread not found" }, { status: 404 });
  }

  const rows = await db()
    .select({ run_id: work_message.run_id, role: work_message.role })
    .from(work_message)
    .where(eq(work_message.id, messageId))
    .limit(1);
  const target = rows[0];
  if (!target || !target.run_id) {
    return NextResponse.json({ error: "Message not found" }, { status: 404 });
  }
  if (target.role !== "user") {
    return NextResponse.json(
      { error: "Only user messages can be truncated from" },
      { status: 400 },
    );
  }

  const result = await truncateWorkThreadFromRun(orgId, threadId, target.run_id);
  if (!result.ok) {
    return NextResponse.json({ error: "Run not found" }, { status: 404 });
  }
  return NextResponse.json({ ok: true });
}
