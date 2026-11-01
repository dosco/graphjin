import { NextResponse } from "next/server";
import { getActionPolicy, updateActionPolicy } from "@neko/llm/workflows";
import { and, db, eq, user_group } from "@neko/db";
import { getOrgId } from "@/lib/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { parseTargetPatterns } from "@/lib/target-patterns";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ policyId: string }>;
};

export async function GET(_: Request, context: RouteContext) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const { policyId } = await context.params;
  const orgId = await getOrgId();
  const policy = await getActionPolicy(orgId, policyId);
  if (!policy) {
    return NextResponse.json({ error: "not found" }, { status: 404 });
  }
  return NextResponse.json({
    policy: {
      id: policy.id,
      name: policy.name,
      description: policy.description,
      appliesToKinds: policy.appliesToKinds,
      appliesToScopes: policy.appliesToScopes,
      mode: policy.mode,
      riskThresholdAutoApprove: policy.riskThresholdAutoApprove,
      allowedTargets: policy.allowedTargets,
      deniedTargets: policy.deniedTargets,
      limits: policy.limits,
      approverGroupId: policy.approverGroupId,
      priority: policy.priority,
      enabled: policy.enabled,
      createdByThreadId: policy.createdByThreadId,
      createdByRunId: policy.createdByRunId,
      createdAt: policy.createdAt.toISOString(),
      updatedAt: policy.updatedAt.toISOString(),
    },
  });
}

/** Changes who may approve requests that match this rule. */
export async function PATCH(request: Request, context: RouteContext) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  const { policyId } = await context.params;
  const body = (await request.json().catch(() => null)) as {
    approverGroupId?: unknown;
    allowedTargets?: unknown;
  } | null;
  if (!body || (!("approverGroupId" in body) && !("allowedTargets" in body))) {
    return NextResponse.json({ error: "send approverGroupId or allowedTargets" }, { status: 400 });
  }
  const orgId = await getOrgId();
  const patch: Parameters<typeof updateActionPolicy>[2] = {};

  if ("approverGroupId" in body) {
    const approverGroupId = body.approverGroupId;
    if (approverGroupId !== null && typeof approverGroupId !== "string") {
      return NextResponse.json({ error: "approverGroupId must be a group id or null" }, { status: 400 });
    }
    if (approverGroupId) {
      const [group] = await db()
        .select({ slug: user_group.slug })
        .from(user_group)
        .where(and(eq(user_group.org_id, orgId), eq(user_group.id, approverGroupId)))
        .limit(1);
      if (!group) return NextResponse.json({ error: "group not found" }, { status: 404 });
    }
    patch.approverGroupId = approverGroupId;
  }

  if ("allowedTargets" in body) {
    const targets = body.allowedTargets as { patterns?: unknown; onMiss?: unknown } | null;
    if (targets === null) {
      patch.allowedTargets = null;
    } else {
      const parsed = parseTargetPatterns(targets?.patterns);
      if (!parsed.ok) return NextResponse.json({ error: parsed.error }, { status: 400 });
      if (targets?.onMiss !== "next" && targets?.onMiss !== "deny") {
        return NextResponse.json({ error: 'onMiss must be "next" or "deny"' }, { status: 400 });
      }
      patch.allowedTargets =
        parsed.patterns.length === 0
          ? null
          : { patterns: parsed.patterns, ...(targets.onMiss === "next" ? { on_miss: "next" } : {}) };
    }
  }

  const policy = await updateActionPolicy(orgId, policyId, patch);
  if (!policy) return NextResponse.json({ error: "not found" }, { status: 404 });
  return NextResponse.json({
    policy: { id: policy.id, approverGroupId: policy.approverGroupId, allowedTargets: policy.allowedTargets },
  });
}
