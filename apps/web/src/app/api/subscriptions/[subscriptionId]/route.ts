import { NextResponse } from "next/server";
import { and, db, eq, subscription } from "@neko/db";
import {
  deleteSubscription,
  setSubscriptionEnabled,
} from "@neko/llm/workflows";
import { getOrgId } from "@/lib/db";
import { requireWorkflow } from "@/lib/entitlements";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ subscriptionId: string }>;
};

async function getOwnedSubscription(orgId: string, id: string) {
  const rows = await db()
    .select()
    .from(subscription)
    .where(and(eq(subscription.org_id, orgId), eq(subscription.id, id)))
    .limit(1);
  return rows[0] ?? null;
}

export async function PATCH(req: Request, context: RouteContext) {
  const { subscriptionId } = await context.params;
  const body = await req.json().catch(() => ({}));
  const orgId = await getOrgId();
  const sub = await getOwnedSubscription(orgId, subscriptionId);
  if (!sub || (await requireWorkflow(sub.workflow_id))) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  if (typeof body.enabled === "boolean") {
    await setSubscriptionEnabled(subscriptionId, body.enabled);
  }
  return NextResponse.json({ ok: true });
}

export async function DELETE(_req: Request, context: RouteContext) {
  const { subscriptionId } = await context.params;
  const orgId = await getOrgId();
  const sub = await getOwnedSubscription(orgId, subscriptionId);
  if (!sub || (await requireWorkflow(sub.workflow_id))) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  await deleteSubscription(subscriptionId);
  return NextResponse.json({ ok: true });
}
