import { pool } from "@neko/db";
import { enableWorkflowApiAccess, getWorkflowApiAccess, updateWorkflowApiLimits } from "../api-access";
import { recordAuditEvent } from "../audit-chain";
import { sha256Hex } from "./contract";

export class CompatImportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "CompatImportError";
  }
}

export type CompatWebhookRow = {
  reckonWorkflowId: string;
  workflowId: string;
  workflowName: string;
  enabled: boolean;
  params: string[] | null;
  batchChunkSize: number;
  updatedAt: string;
};

/** Reckon-scale runs: the ceilings this deployment allows, not the API defaults. */
const COMPAT_LIMITS = {
  requestLimitPerMinute: 60,
  pollLimitPerMinute: 60,
  queueCap: 100,
  concurrencyCap: 5,
  maxRequestBytes: 256 * 1024,
  maxRuntimeSeconds: 1_800,
  maxModelCalls: 1_024,
  maxToolCalls: 1_024,
  maxTokensPerRun: 3_000_000,
  maxCostMicrosPerRun: 100_000_000,
  maxArtifactBytes: 50 * 1024 * 1024,
  retentionHours: 168,
};

export async function listCompatWebhooks(orgId: string): Promise<CompatWebhookRow[]> {
  const { rows } = await pool().query<{
    reckon_workflow_id: string;
    workflow_id: string;
    name: string;
    enabled: boolean;
    params: string[] | null;
    batch_chunk_size: number;
    updated_at: Date;
  }>(
    `select compat.reckon_workflow_id, compat.workflow_id, workflow.name, compat.enabled,
            compat.params, compat.batch_chunk_size, compat.updated_at
       from compat_webhook compat
       join workflow_definition workflow on workflow.id = compat.workflow_id
      where compat.org_id = $1
      order by workflow.name`,
    [orgId],
  );
  return rows.map((row) => ({
    reckonWorkflowId: row.reckon_workflow_id,
    workflowId: row.workflow_id,
    workflowName: row.name,
    enabled: row.enabled,
    params: row.params,
    batchChunkSize: row.batch_chunk_size,
    updatedAt: row.updated_at.toISOString(),
  }));
}

/**
 * Map one Reckon webhook onto an OpenNeko workflow. The token is stored as a
 * digest, so callers keep the token they already have and OpenNeko never holds it.
 */
export async function importCompatWebhook(input: {
  orgId: string;
  actorUserId: string | null;
  reckonWorkflowId: string;
  workflowId: string;
  token: string;
  params?: string[] | null;
  batchChunkSize?: number;
  enabled?: boolean;
}): Promise<CompatWebhookRow> {
  const reckonWorkflowId = input.reckonWorkflowId.trim();
  if (!/^[A-Za-z0-9._:-]{1,120}$/.test(reckonWorkflowId)) {
    throw new CompatImportError("The Reckon workflow id must be 1 to 120 plain identifier characters.");
  }
  if (input.token.trim().length < 16) {
    throw new CompatImportError("The webhook token must be at least 16 characters.");
  }
  const chunk = input.batchChunkSize ?? 1_000;
  if (!Number.isInteger(chunk) || chunk < 1 || chunk > 1_000) {
    throw new CompatImportError("The batch chunk size must be a whole number from 1 to 1000.");
  }
  if (input.params && input.params.some((param) => !/^[A-Za-z0-9_]{1,64}$/.test(param))) {
    throw new CompatImportError("Each parameter name must be 1 to 64 letters, numbers or underscores.");
  }
  const workflow = await pool().query<{ id: string; name: string }>(
    "select id, name from workflow_definition where org_id = $1 and id = $2::uuid",
    [input.orgId, input.workflowId],
  );
  if (!workflow.rows[0]) throw new CompatImportError("Workflow not found.");

  const access = await getWorkflowApiAccess(input.orgId, input.workflowId);
  if (!access?.enabled) {
    await enableWorkflowApiAccess({
      orgId: input.orgId,
      workflowId: input.workflowId,
      actor: { userId: input.actorUserId, role: "admin" },
    });
  }
  await updateWorkflowApiLimits({
    orgId: input.orgId,
    workflowId: input.workflowId,
    actor: { userId: input.actorUserId, role: "admin" },
    limits: COMPAT_LIMITS,
  });

  await pool().query(
    `insert into compat_webhook (
       reckon_workflow_id, org_id, workflow_id, token_sha256, enabled, params,
       batch_chunk_size, created_by_user_id, updated_at
     ) values ($1, $2, $3::uuid, $4, $5, $6, $7, $8, now())
     on conflict (reckon_workflow_id) do update
       set org_id = excluded.org_id,
           workflow_id = excluded.workflow_id,
           token_sha256 = excluded.token_sha256,
           enabled = excluded.enabled,
           params = excluded.params,
           batch_chunk_size = excluded.batch_chunk_size,
           updated_at = now()`,
    [
      reckonWorkflowId,
      input.orgId,
      input.workflowId,
      sha256Hex(input.token),
      input.enabled ?? true,
      input.params ?? null,
      chunk,
      input.actorUserId,
    ],
  );
  await recordAuditEvent({
    orgId: input.orgId,
    entityKind: "compat_webhook",
    entityId: reckonWorkflowId,
    event: "imported",
    payload: {
      actorUserId: input.actorUserId,
      workflowId: input.workflowId,
      params: input.params ?? null,
      batchChunkSize: chunk,
    },
  });
  const rows = await listCompatWebhooks(input.orgId);
  const saved = rows.find((row) => row.reckonWorkflowId === reckonWorkflowId);
  if (!saved) throw new CompatImportError("The webhook could not be read back after saving.");
  return saved;
}

export async function removeCompatWebhook(input: {
  orgId: string;
  actorUserId: string | null;
  reckonWorkflowId: string;
}): Promise<boolean> {
  const { rowCount } = await pool().query(
    "delete from compat_webhook where org_id = $1 and reckon_workflow_id = $2",
    [input.orgId, input.reckonWorkflowId],
  );
  if (!rowCount) return false;
  await recordAuditEvent({
    orgId: input.orgId,
    entityKind: "compat_webhook",
    entityId: input.reckonWorkflowId,
    event: "removed",
    payload: { actorUserId: input.actorUserId },
  });
  return true;
}
