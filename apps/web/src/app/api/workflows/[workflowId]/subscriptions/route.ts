import { NextRequest, NextResponse } from "next/server";
import {
  checkSubscriptionWouldLoop,
  createSubscription,
  detectMutationLoop,
  getDataSourceForOrg,
  getWorkflow,
  listSubscriptionsByWorkflow,
  parseSourceChangeFilter,
  SubscriptionSelfLoopError,
  type SubscriptionSourceKind,
} from "@neko/llm/workflows";
import { getOrgId } from "@/lib/db";
import { requireWorkflow } from "@/lib/entitlements";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ workflowId: string }>;
};

const VALID_SOURCE_KINDS: SubscriptionSourceKind[] = [
  "workflow_output",
  "source_change",
  "external_event",
];

export async function GET(_req: NextRequest, context: RouteContext) {
  const { workflowId } = await context.params;
  const deniedWorkflow = await requireWorkflow(workflowId);
  if (deniedWorkflow) return deniedWorkflow;
  const orgId = await getOrgId();
  const subs = await listSubscriptionsByWorkflow(orgId, workflowId);
  return NextResponse.json({
    subscriptions: subs.map((s) => ({
      id: s.id,
      workflowId: s.workflowId,
      sourceKind: s.sourceKind,
      filter: s.filter,
      enabled: s.enabled,
      debounceMs: s.debounceMs,
      maxConcurrentRuns: s.maxConcurrentRuns,
      maxChainDepthOverride: s.maxChainDepthOverride,
      idempotencyKeyTemplate: s.idempotencyKeyTemplate,
      createdAt: s.createdAt.toISOString(),
      updatedAt: s.updatedAt.toISOString(),
    })),
  });
}

export async function POST(req: NextRequest, context: RouteContext) {
  const { workflowId } = await context.params;
  const deniedWorkflow = await requireWorkflow(workflowId);
  if (deniedWorkflow) return deniedWorkflow;
  const body = await req.json().catch(() => ({}));
  const sourceKind = body.sourceKind as string | undefined;
  if (!sourceKind || !VALID_SOURCE_KINDS.includes(sourceKind as SubscriptionSourceKind)) {
    return NextResponse.json(
      { error: `sourceKind required; one of ${VALID_SOURCE_KINDS.join(", ")}` },
      { status: 400 },
    );
  }
  const orgId = await getOrgId();
  const filter =
    typeof body.filter === "object" && body.filter !== null
      ? (body.filter as Record<string, unknown>)
      : {};

  if (sourceKind === "workflow_output") {
    try {
      await checkSubscriptionWouldLoop({
        orgId,
        workflowId,
        filter,
      });
    } catch (e) {
      if (e instanceof SubscriptionSelfLoopError) {
        return NextResponse.json(
          {
            error: e.message,
            code: "self_loop",
            matchingOutputIds: e.matchingOutputIds,
          },
          { status: 422 },
        );
      }
      throw e;
    }
  }

  if (sourceKind === "source_change") {
    const parsed = parseSourceChangeFilter(filter);
    if (!parsed) {
      return NextResponse.json(
        {
          error:
            "source_change filter requires { table: string, primary_key: string[] } and optional where/select/version_column (identifiers only)",
          code: "invalid_filter",
        },
        { status: 400 },
      );
    }

    const dataSource = await getDataSourceForOrg(orgId);
    if (!dataSource) {
      return NextResponse.json(
        {
          error:
            "no data_source configured for this org — source_change subscriptions need a configured data source",
          code: "no_data_source",
        },
        { status: 422 },
      );
    }

    if (!body.acknowledgeMutationLoop) {
      const workflow = await getWorkflow(orgId, workflowId);
      if (workflow) {
        const loopCheck = detectMutationLoop({ filter: parsed, workflow });
        const hasIdempotency =
          typeof body.idempotencyKeyTemplate === "string" &&
          body.idempotencyKeyTemplate.length > 0;
        if (loopCheck.loops && !hasIdempotency) {
          return NextResponse.json(
            {
              error: loopCheck.reason,
              code: "mutation_loop",
              mutationKeyword: loopCheck.mutationKeyword,
            },
            { status: 422 },
          );
        }
      }
    }
  }

  const sub = await createSubscription({
    orgId,
    workflowId,
    sourceKind: sourceKind as SubscriptionSourceKind,
    filter,
    enabled: typeof body.enabled === "boolean" ? body.enabled : true,
    debounceMs:
      typeof body.debounceMs === "number" ? body.debounceMs : 0,
    maxConcurrentRuns:
      typeof body.maxConcurrentRuns === "number"
        ? body.maxConcurrentRuns
        : 5,
    maxChainDepthOverride:
      typeof body.maxChainDepthOverride === "number"
        ? body.maxChainDepthOverride
        : null,
    idempotencyKeyTemplate:
      typeof body.idempotencyKeyTemplate === "string"
        ? body.idempotencyKeyTemplate
        : null,
  });
  return NextResponse.json({
    subscription: {
      id: sub.id,
      sourceKind: sub.sourceKind,
      filter: sub.filter,
      enabled: sub.enabled,
    },
  });
}
