import {
  and,
  db,
  desc,
  eq,
  isNotNull,
  sql,
  workflow_api_access,
  workflow_api_admission,
  workflow_definition,
} from "@neko/db";
import {
  DEFAULT_WORKFLOW_API_LIMITS,
  WorkflowApiError,
  issueWorkflowApiToken,
  parseCompiledWorkflowBatchContract,
  verifyWorkflowApiTokenDigest,
  workflowApiLimitPatch,
  type CompiledWorkflowBatchContract,
  type WorkflowApiLimits,
} from "./api-contract";
import { recordAuditEvent } from "./audit-chain";

export type WorkflowApiAccessView = {
  workflowId: string;
  enabled: boolean;
  tokenPrefix: string | null;
  tokenCreatedAt: Date | null;
  tokenRotatedAt: Date | null;
  lastUsedAt: Date | null;
  limits: WorkflowApiLimits;
  batch: {
    available: boolean;
    recordsField: string | null;
    columns: string[];
  };
  createdAt: Date | null;
  updatedAt: Date | null;
};

export type VerifiedWorkflowApiAccess = WorkflowApiAccessView & {
  orgId: string;
  verifier: string;
  outputContract: Record<string, unknown> | null;
  batchContract: CompiledWorkflowBatchContract | null;
};

type AccessRow = typeof workflow_api_access.$inferSelect;

function rowLimits(row: AccessRow | null): WorkflowApiLimits {
  if (!row) return { ...DEFAULT_WORKFLOW_API_LIMITS };
  return {
    requestLimitPerMinute: row.request_limit_per_minute,
    pollLimitPerMinute: row.poll_limit_per_minute,
    queueCap: row.queue_cap,
    concurrencyCap: row.concurrency_cap,
    batchMaxRecords: row.batch_max_records,
    batchChunkSize: row.batch_chunk_size,
    maxRequestBytes: row.max_request_bytes,
    maxResultBytes: row.max_result_bytes,
    maxArtifactBytes: row.max_artifact_bytes,
    maxRuntimeSeconds: row.max_runtime_seconds,
    maxModelCalls: row.max_model_calls,
    maxToolCalls: row.max_tool_calls,
    maxTokensPerRun: row.max_tokens_per_run,
    maxCostMicrosPerRun: row.max_cost_micros_per_run,
    rollingWindowSeconds: row.rolling_window_seconds,
    rollingTokenBudget: row.rolling_token_budget,
    rollingCostMicrosBudget: row.rolling_cost_micros_budget,
    retentionHours: row.retention_hours,
  };
}

function toAccessView(input: {
  workflowId: string;
  row: AccessRow | null;
  outputContract: Record<string, unknown> | null;
}): WorkflowApiAccessView {
  const batchContract = parseCompiledWorkflowBatchContract(input.outputContract);
  return {
    workflowId: input.workflowId,
    enabled: input.row?.enabled ?? false,
    tokenPrefix: input.row?.token_prefix ?? null,
    tokenCreatedAt: input.row?.token_created_at ?? null,
    tokenRotatedAt: input.row?.token_rotated_at ?? null,
    lastUsedAt: input.row?.last_used_at ?? null,
    limits: rowLimits(input.row),
    batch: {
      available: batchContract !== null,
      recordsField: batchContract?.recordsField ?? null,
      columns: batchContract?.columns.map((column) => column.name) ?? [],
    },
    createdAt: input.row?.created_at ?? null,
    updatedAt: input.row?.updated_at ?? null,
  };
}

export async function getWorkflowApiAccess(
  orgId: string,
  workflowId: string,
): Promise<WorkflowApiAccessView | null> {
  const [workflow] = await db()
    .select({
      id: workflow_definition.id,
      outputContract: workflow_definition.output_contract,
    })
    .from(workflow_definition)
    .where(
      and(
        eq(workflow_definition.org_id, orgId),
        eq(workflow_definition.id, workflowId),
      ),
    )
    .limit(1);
  if (!workflow) return null;
  const [row] = await db()
    .select()
    .from(workflow_api_access)
    .where(
      and(
        eq(workflow_api_access.org_id, orgId),
        eq(workflow_api_access.workflow_id, workflowId),
      ),
    )
    .limit(1);
  return toAccessView({
    workflowId,
    row: row ?? null,
    outputContract:
      (workflow.outputContract as Record<string, unknown> | null) ?? null,
  });
}

/** The body of the workflow's latest single-mode API call, shown as its request example. */
export async function getLastWorkflowApiRequest(
  orgId: string,
  workflowId: string,
): Promise<Record<string, unknown> | null> {
  const [row] = await db()
    .select({ payload: workflow_api_admission.request_payload })
    .from(workflow_api_admission)
    .where(
      and(
        eq(workflow_api_admission.org_id, orgId),
        eq(workflow_api_admission.workflow_id, workflowId),
        eq(workflow_api_admission.execution_mode, "single"),
        isNotNull(workflow_api_admission.request_payload),
      ),
    )
    .orderBy(desc(workflow_api_admission.created_at))
    .limit(1);
  const payload = row?.payload;
  return payload && typeof payload === "object" && !Array.isArray(payload)
    ? (payload as Record<string, unknown>)
    : null;
}

/** Public verifier intentionally looks up by opaque workflow UUID only. */
export async function verifyWorkflowApiAccessToken(
  workflowId: string,
  token: string,
): Promise<VerifiedWorkflowApiAccess | null> {
  const [found] = await db()
    .select({
      access: workflow_api_access,
      outputContract: workflow_definition.output_contract,
    })
    .from(workflow_api_access)
    .innerJoin(
      workflow_definition,
      and(
        eq(workflow_definition.id, workflow_api_access.workflow_id),
        eq(workflow_definition.org_id, workflow_api_access.org_id),
      ),
    )
    .where(eq(workflow_api_access.workflow_id, workflowId))
    .limit(1);

  const verifier = found?.access.token_verifier;
  const matches = verifyWorkflowApiTokenDigest(token, verifier);
  if (!found || !found.access.enabled || !verifier || !matches) return null;
  const outputContract =
    (found.outputContract as Record<string, unknown> | null) ?? null;
  return {
    ...toAccessView({ workflowId, row: found.access, outputContract }),
    orgId: found.access.org_id,
    verifier,
    outputContract,
    batchContract: parseCompiledWorkflowBatchContract(outputContract),
  };
}

type LifecycleActor = { userId: string | null; role: string };
type TokenLifecycleAction = "enable" | "rotate";

async function issueAndStoreToken(input: {
  orgId: string;
  workflowId: string;
  actor: LifecycleActor;
  action: TokenLifecycleAction;
}): Promise<{ access: WorkflowApiAccessView; token: string }> {
  const issued = issueWorkflowApiToken();
  const now = new Date();
  await db().transaction(async (tx) => {
    const locked = await tx.execute<{ id: string }>(sql`
      select id
      from workflow_definition
      where id = ${input.workflowId} and org_id = ${input.orgId}
      for update
    `);
    if (locked.rows.length === 0) {
      throw new WorkflowApiError(
        "workflow_not_found",
        "Workflow not found.",
        404,
      );
    }
    const [current] = await tx
      .select({ enabled: workflow_api_access.enabled })
      .from(workflow_api_access)
      .where(
        and(
          eq(workflow_api_access.org_id, input.orgId),
          eq(workflow_api_access.workflow_id, input.workflowId),
        ),
      )
      .limit(1);
    if (input.action === "enable" && current?.enabled) {
      throw new WorkflowApiError(
        "api_access_already_enabled",
        "API access is already enabled. Rotate the token to replace it.",
        409,
      );
    }
    if (input.action === "rotate" && !current?.enabled) {
      throw new WorkflowApiError(
        "api_access_disabled",
        "Enable API access before rotating its token.",
        409,
      );
    }
    await tx
      .insert(workflow_api_access)
      .values({
        workflow_id: input.workflowId,
        org_id: input.orgId,
        enabled: true,
        token_verifier: issued.verifier,
        token_prefix: issued.prefix,
        token_created_at: now,
        token_rotated_at: input.action === "rotate" ? now : null,
        updated_at: now,
      })
      .onConflictDoUpdate({
        target: workflow_api_access.workflow_id,
        set: {
          org_id: input.orgId,
          enabled: true,
          token_verifier: issued.verifier,
          token_prefix: issued.prefix,
          token_created_at: now,
          token_rotated_at: input.action === "rotate" ? now : null,
          updated_at: now,
        },
      });
  });

  await recordAuditEvent({
    orgId: input.orgId,
    entityKind: "workflow_api_access",
    entityId: input.workflowId,
    event: input.action === "enable" ? "enabled" : "token_rotated",
    payload: {
      actorUserId: input.actor.userId,
      actorRole: input.actor.role,
      tokenPrefix: issued.prefix,
    },
  });
  const access = await getWorkflowApiAccess(input.orgId, input.workflowId);
  if (!access) throw new Error("Workflow API access disappeared after update.");
  return { access, token: issued.token };
}

export async function enableWorkflowApiAccess(input: {
  orgId: string;
  workflowId: string;
  actor: LifecycleActor;
}): Promise<{ access: WorkflowApiAccessView; token: string }> {
  return issueAndStoreToken({ ...input, action: "enable" });
}

export async function rotateWorkflowApiToken(input: {
  orgId: string;
  workflowId: string;
  actor: LifecycleActor;
}): Promise<{ access: WorkflowApiAccessView; token: string }> {
  return issueAndStoreToken({ ...input, action: "rotate" });
}

export async function disableWorkflowApiAccess(input: {
  orgId: string;
  workflowId: string;
  actor: LifecycleActor;
}): Promise<WorkflowApiAccessView> {
  const now = new Date();
  const rows = await db()
    .update(workflow_api_access)
    .set({ enabled: false, updated_at: now })
    .where(
      and(
        eq(workflow_api_access.org_id, input.orgId),
        eq(workflow_api_access.workflow_id, input.workflowId),
      ),
    )
    .returning({ id: workflow_api_access.workflow_id });
  if (rows.length === 0) {
    const workflow = await getWorkflowApiAccess(input.orgId, input.workflowId);
    if (!workflow) {
      throw new WorkflowApiError(
        "workflow_not_found",
        "Workflow not found.",
        404,
      );
    }
    return workflow;
  }
  await recordAuditEvent({
    orgId: input.orgId,
    entityKind: "workflow_api_access",
    entityId: input.workflowId,
    event: "disabled",
    payload: {
      actorUserId: input.actor.userId,
      actorRole: input.actor.role,
    },
  });
  const access = await getWorkflowApiAccess(input.orgId, input.workflowId);
  if (!access) throw new Error("Workflow API access disappeared after disable.");
  return access;
}

const LIMIT_COLUMNS = {
  requestLimitPerMinute: "request_limit_per_minute",
  pollLimitPerMinute: "poll_limit_per_minute",
  queueCap: "queue_cap",
  concurrencyCap: "concurrency_cap",
  batchMaxRecords: "batch_max_records",
  batchChunkSize: "batch_chunk_size",
  maxRequestBytes: "max_request_bytes",
  maxResultBytes: "max_result_bytes",
  maxArtifactBytes: "max_artifact_bytes",
  maxRuntimeSeconds: "max_runtime_seconds",
  maxModelCalls: "max_model_calls",
  maxToolCalls: "max_tool_calls",
  maxTokensPerRun: "max_tokens_per_run",
  maxCostMicrosPerRun: "max_cost_micros_per_run",
  rollingWindowSeconds: "rolling_window_seconds",
  rollingTokenBudget: "rolling_token_budget",
  rollingCostMicrosBudget: "rolling_cost_micros_budget",
  retentionHours: "retention_hours",
} as const satisfies Record<keyof WorkflowApiLimits, keyof AccessRow>;

export async function updateWorkflowApiLimits(input: {
  orgId: string;
  workflowId: string;
  actor: LifecycleActor;
  limits: unknown;
}): Promise<WorkflowApiAccessView> {
  const patch = workflowApiLimitPatch(input.limits);
  if (Object.keys(patch).length === 0) {
    throw new WorkflowApiError(
      "empty_limits",
      "At least one limit is required.",
      400,
    );
  }
  const current = await getWorkflowApiAccess(input.orgId, input.workflowId);
  if (!current) {
    throw new WorkflowApiError("workflow_not_found", "Workflow not found.", 404);
  }
  const merged = { ...current.limits, ...patch };
  if (merged.batchChunkSize > merged.batchMaxRecords) {
    throw new WorkflowApiError(
      "invalid_batch_limits",
      "batchChunkSize cannot exceed batchMaxRecords.",
      400,
    );
  }
  const values: Record<string, number | Date | string | boolean> = {
    workflow_id: input.workflowId,
    org_id: input.orgId,
    enabled: current.enabled,
    updated_at: new Date(),
  };
  for (const [key, value] of Object.entries(patch) as Array<
    [keyof WorkflowApiLimits, number]
  >) {
    values[LIMIT_COLUMNS[key]] = value;
  }
  // Postgres evaluates workflow_api_access_token_state on the proposed row
  // before it resolves the conflict, so an upsert without the token columns
  // fails for a workflow whose API access is enabled. Update the row in place.
  const [existing] = await db()
    .select({ workflowId: workflow_api_access.workflow_id })
    .from(workflow_api_access)
    .where(
      and(
        eq(workflow_api_access.org_id, input.orgId),
        eq(workflow_api_access.workflow_id, input.workflowId),
      ),
    )
    .limit(1);
  if (existing) {
    await db()
      .update(workflow_api_access)
      .set(values as Partial<typeof workflow_api_access.$inferInsert>)
      .where(
        and(
          eq(workflow_api_access.org_id, input.orgId),
          eq(workflow_api_access.workflow_id, input.workflowId),
        ),
      );
  } else {
    await db()
      .insert(workflow_api_access)
      .values(values as typeof workflow_api_access.$inferInsert);
  }
  await recordAuditEvent({
    orgId: input.orgId,
    entityKind: "workflow_api_access",
    entityId: input.workflowId,
    event: "limits_updated",
    payload: {
      actorUserId: input.actor.userId,
      actorRole: input.actor.role,
      fields: Object.keys(patch).sort(),
    },
  });
  const access = await getWorkflowApiAccess(input.orgId, input.workflowId);
  if (!access) throw new Error("Workflow API access disappeared after update.");
  return access;
}
