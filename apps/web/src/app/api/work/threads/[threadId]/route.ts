import { NextResponse } from "next/server";
import { getOrgId } from "@/lib/db";
import { deleteWorkThread, getWorkThreadBundle } from "@/lib/work-store";
import { getAuthorizedWorkThread } from "@/lib/work-thread-auth";

type RouteContext = {
  params: Promise<{ threadId: string }>;
};

const PAGE_MAX = 100;

function pageFrom(url: URL) {
  const limit = Number(url.searchParams.get("limit"));
  if (!Number.isInteger(limit) || limit < 1) return undefined;
  return { limit: Math.min(limit, PAGE_MAX), beforeRunId: url.searchParams.get("before") };
}

export async function GET(request: Request, context: RouteContext) {
  const { threadId } = await context.params;
  const orgId = await getOrgId();
  const authorized = await getAuthorizedWorkThread(orgId, threadId);
  if (!authorized) {
    return NextResponse.json({ error: "Thread not found" }, { status: 404 });
  }
  const bundle = await getWorkThreadBundle(orgId, threadId, pageFrom(new URL(request.url)));
  if (!bundle) {
    return NextResponse.json({ error: "Thread not found" }, { status: 404 });
  }
  return NextResponse.json(bundle);
}

export async function DELETE(_: Request, context: RouteContext) {
  const { threadId } = await context.params;
  const orgId = await getOrgId();
  const authorized = await getAuthorizedWorkThread(orgId, threadId);
  if (!authorized) {
    return NextResponse.json({ error: "Thread not found" }, { status: 404 });
  }
  const ok = await deleteWorkThread(orgId, threadId);
  if (!ok) {
    return NextResponse.json({ error: "Thread not found" }, { status: 404 });
  }
  return NextResponse.json({ ok: true });
}
