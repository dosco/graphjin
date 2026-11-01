import { NextRequest, NextResponse } from "next/server";
import { and, db, desc, eq, observation, workflow_definition } from "@neko/db";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { workflowVisibility } from "@/lib/entitlements";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(req: NextRequest) {
  const orgId = await getOrgId();
  const url = new URL(req.url);
  const consumerWorkflowId = url.searchParams.get("consumerWorkflowId");
  const sourceOutputId = url.searchParams.get("sourceOutputId");
  const status = url.searchParams.get("status") ?? null;
  const limit = Math.min(
    Math.max(Number(url.searchParams.get("limit") ?? "50"), 1),
    500,
  );

  const filters = [eq(observation.org_id, orgId)];
  if (consumerWorkflowId)
    filters.push(eq(observation.consumer_workflow_id, consumerWorkflowId));
  if (sourceOutputId)
    filters.push(eq(observation.source_output_id, sourceOutputId));
  if (status) filters.push(eq(observation.status, status));

  const rows = await db()
    .select()
    .from(observation)
    .where(and(...filters))
    .orderBy(desc(observation.created_at))
    .limit(limit);

  const visible = await workflowVisibility();
  const actorUserId = (await getCurrentActor()).userId;
  const owners = new Map(
    (await db()
      .select({ id: workflow_definition.id, ownerUserId: workflow_definition.owner_user_id })
      .from(workflow_definition)
      .where(eq(workflow_definition.org_id, orgId))).map((w) => [w.id, w.ownerUserId]),
  );
  const shown = rows.filter((r) =>
    r.consumer_workflow_id
      ? visible({ id: r.consumer_workflow_id, ownerUserId: owners.get(r.consumer_workflow_id) ?? "" })
      : !r.consumer_user_id || r.consumer_user_id === actorUserId,
  );
  return NextResponse.json({
    observations: shown.map((r) => ({
      id: r.id,
      sourceOutputId: r.source_output_id,
      consumerKind: r.consumer_kind,
      consumerWorkflowId: r.consumer_workflow_id,
      consumerRunId: r.consumer_run_id,
      consumerUserId: r.consumer_user_id,
      subscriptionId: r.subscription_id,
      title: r.title,
      body: r.body,
      mood: r.mood,
      status: r.status,
      firstSeenAt: r.first_seen_at.toISOString(),
      lastSeenAt: r.last_seen_at.toISOString(),
      createdAt: r.created_at.toISOString(),
    })),
  });
}
