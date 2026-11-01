import { NextResponse } from "next/server";
import { action_policy, asc, db, eq } from "@neko/db";
import { getOrgId } from "@/lib/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const orgId = await getOrgId();
  const rows = await db()
    .select()
    .from(action_policy)
    .where(eq(action_policy.org_id, orgId))
    .orderBy(asc(action_policy.priority), asc(action_policy.name));

  return NextResponse.json({
    policies: rows.map((r) => ({
      id: r.id,
      name: r.name,
      description: r.description,
      appliesToKinds: r.applies_to_kinds,
      appliesToScopes: r.applies_to_scopes,
      mode: r.mode,
      riskThresholdAutoApprove: r.risk_threshold_auto_approve,
      allowedTargets: r.allowed_targets,
      deniedTargets: r.denied_targets,
      limits: r.limits,
      approverGroupId: r.approver_group_id,
      priority: r.priority,
      enabled: r.enabled,
      createdAt: r.created_at.toISOString(),
      updatedAt: r.updated_at.toISOString(),
    })),
  });
}
