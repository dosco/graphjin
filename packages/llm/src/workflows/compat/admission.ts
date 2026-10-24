import { randomUUID } from "node:crypto";
import { pool } from "@neko/db";
import type { PoolClient } from "pg";
import { admitRunSpend, recordBudgetBlocked, SpendBudgetExceeded } from "../../spend/admission";
import {
  buildReckonSeedMessage,
  reckonSeedPayload,
  ReckonWebhookError,
  reckonRequestFingerprint,
  sha256Hex,
  ulid,
  type ReckonExecutionMode,
} from "./contract";

export type CompatWebhook = {
  reckonWorkflowId: string;
  orgId: string;
  workflowId: string;
  tokenSha256: string;
  enabled: boolean;
  params: string[] | null;
  batchChunkSize: number;
};

export type ReckonAdmission = {
  runId: string;
  status: "queued" | "running";
  mode: ReckonExecutionMode;
  replay: boolean;
  immediatelyEligible: boolean;
};

/** Reckon production values; the same env vars set them. */
function reckonLimits() {
  const value = (name: string, fallback: number, max = Number.MAX_SAFE_INTEGER) => {
    const parsed = Number(process.env[name]);
    return Number.isInteger(parsed) && parsed > 0 ? Math.min(parsed, max) : fallback;
  };
  const queueMax = value("HEADLESS_QUEUE_MAX", 250);
  const perWorkflow = value("HEADLESS_QUEUE_MAX_PER_WORKFLOW", 100);
  return {
    queueMax,
    queueMaxPerWorkflow: Math.min(perWorkflow, queueMax),
    internalReservedSlots: Math.min(value("HEADLESS_INTERNAL_RESERVED_SLOTS", 10), Math.max(0, queueMax - 1)),
    webhookRatePerMinute: value("HEADLESS_WEBHOOK_RATE_PER_MINUTE", 10),
    globalWebhookRatePerMinute: value("HEADLESS_GLOBAL_WEBHOOK_RATE_PER_MINUTE", 50),
    resultRatePerMinute: value("HEADLESS_RESULT_RATE_PER_MINUTE", 10),
    maxConcurrency: value("HEADLESS_MAX_CONCURRENCY", 5, 10),
  };
}

export async function getCompatWebhook(reckonWorkflowId: string): Promise<CompatWebhook | null> {
  const { rows } = await pool().query<{
    reckon_workflow_id: string;
    org_id: string;
    workflow_id: string;
    token_sha256: string;
    enabled: boolean;
    params: string[] | null;
    batch_chunk_size: number;
  }>(
    `select reckon_workflow_id, org_id, workflow_id, token_sha256, enabled, params, batch_chunk_size
       from compat_webhook where reckon_workflow_id = $1`,
    [reckonWorkflowId],
  );
  const row = rows[0];
  if (!row || !row.enabled) return null;
  return {
    reckonWorkflowId: row.reckon_workflow_id,
    orgId: row.org_id,
    workflowId: row.workflow_id,
    tokenSha256: row.token_sha256,
    enabled: row.enabled,
    params: row.params,
    batchChunkSize: row.batch_chunk_size,
  };
}

function minuteWindow(now: Date): { start: Date; retryAfterSeconds: number } {
  const start = new Date(Math.floor(now.getTime() / 60_000) * 60_000);
  return {
    start,
    retryAfterSeconds: Math.max(1, Math.ceil((start.getTime() + 60_000 - now.getTime()) / 1_000)),
  };
}

async function consumeReckonBucket(
  client: Pick<PoolClient, "query">,
  input: { scopeId: string; operation: "invoke" | "poll"; limit: number; windowStart: Date },
): Promise<boolean> {
  const { rows } = await client.query<{ count: number }>(
    `insert into workflow_api_rate_bucket (scope_kind, scope_id, operation, window_start, count, expires_at)
     values ('workflow', $1, $2, $3::timestamptz, 1, $3::timestamptz + interval '2 minutes')
     on conflict (scope_kind, scope_id, operation, window_start)
     do update set count = workflow_api_rate_bucket.count + 1
     returning count`,
    [input.scopeId, input.operation, input.windowStart],
  );
  return (rows[0]?.count ?? 0) <= input.limit;
}

/**
 * Reckon consumes the start rate before anything else, including a replay, so
 * a caller cannot dodge the limit by repeating one idempotency key.
 */
export async function consumeReckonStartRate(
  webhook: CompatWebhook,
  now = new Date(),
): Promise<void> {
  const limits = reckonLimits();
  const window = minuteWindow(now);
  const client = await pool().connect();
  try {
    const global = await consumeReckonBucket(client, {
      scopeId: "reckon:global",
      operation: "invoke",
      limit: limits.globalWebhookRatePerMinute,
      windowStart: window.start,
    });
    if (!global) {
      throw new ReckonWebhookError(429, { error: "global_rate" }, window.retryAfterSeconds);
    }
    const workflow = await consumeReckonBucket(client, {
      scopeId: `reckon:${webhook.reckonWorkflowId}`,
      operation: "invoke",
      limit: limits.webhookRatePerMinute,
      windowStart: window.start,
    });
    if (!workflow) {
      throw new ReckonWebhookError(429, { error: "workflow_rate" }, window.retryAfterSeconds);
    }
  } finally {
    client.release();
  }
}

export async function consumeReckonResultRate(
  webhook: CompatWebhook,
  now = new Date(),
): Promise<void> {
  const limits = reckonLimits();
  const window = minuteWindow(now);
  const allowed = await consumeReckonBucket(pool(), {
    scopeId: `reckon:${webhook.reckonWorkflowId}`,
    operation: "poll",
    limit: limits.resultRatePerMinute,
    windowStart: window.start,
  });
  if (!allowed) {
    throw new ReckonWebhookError(429, { error: "rate_limited" }, window.retryAfterSeconds);
  }
}

const BUDGET_REASON: Record<string, string> = {
  org_hourly: "global_hourly_budget",
  org_daily: "global_daily_budget",
  workflow_hourly: "workflow_hourly_budget",
  workflow_daily: "workflow_daily_budget",
};

function budgetRejection(error: SpendBudgetExceeded): ReckonWebhookError {
  return new ReckonWebhookError(
    429,
    {
      error: BUDGET_REASON[error.budget] ?? "workflow_daily_budget",
      limitUsd: error.limitUsd,
      committedUsd: error.committedUsd,
      reservationUsd: error.reservationUsd,
    },
    error.retryAfterSeconds,
  );
}

export async function admitReckonWebhookRun(input: {
  webhook: CompatWebhook;
  mode: ReckonExecutionMode;
  params: Record<string, unknown>;
  idempotencyKey: string;
  requestBytes: number;
  now?: Date;
}): Promise<ReckonAdmission> {
  const now = input.now ?? new Date();
  const fingerprint = reckonRequestFingerprint(input.mode, input.params);
  const client = await pool().connect();
  let released = false;
  try {
    await client.query("begin");
    await client.query("select pg_advisory_xact_lock(hashtext($1))", [
      `reckon-webhook:${input.webhook.reckonWorkflowId}:${input.idempotencyKey}`,
    ]);

    const replay = await client.query<{ id: string; request_fingerprint: string; status: string }>(
      `select compat.id, compat.request_fingerprint, run.status
         from compat_webhook_run compat
         join workflow_run run on run.id = compat.workflow_run_id
        where compat.reckon_workflow_id = $1 and compat.idempotency_key = $2`,
      [input.webhook.reckonWorkflowId, input.idempotencyKey],
    );
    const existing = replay.rows[0];
    if (existing) {
      await client.query("commit");
      if (existing.request_fingerprint !== fingerprint) {
        throw new ReckonWebhookError(409, { error: "idempotency_conflict" });
      }
      return {
        runId: existing.id,
        status: existing.status === "running" ? "running" : "queued",
        mode: input.mode,
        replay: true,
        immediatelyEligible: false,
      };
    }

    const access = await client.query<{
      enabled: boolean;
      workflow_enabled: boolean;
      retention_hours: number;
    }>(
      `select access.enabled, workflow.enabled as workflow_enabled, access.retention_hours
         from workflow_api_access access
         join workflow_definition workflow on workflow.id = access.workflow_id
        where access.workflow_id = $1 for update of access`,
      [input.webhook.workflowId],
    );
    if (!access.rows[0]?.enabled) {
      throw new ReckonWebhookError(422, {
        error: "batch_not_ready",
        status: "api_access_disabled",
        message: "The target workflow does not accept API runs.",
      });
    }
    if (!access.rows[0].workflow_enabled) {
      throw new ReckonWebhookError(409, { error: "workflow_disabled" });
    }

    const limits = reckonLimits();
    const outstanding = await client.query<{ total: number; workflow: number }>(
      `select count(*)::int as total,
              count(*) filter (where compat.reckon_workflow_id = $1)::int as workflow
         from compat_webhook_run compat
         join workflow_run run on run.id = compat.workflow_run_id
        where run.status in ('queued', 'running')`,
      [input.webhook.reckonWorkflowId],
    );
    const counts = outstanding.rows[0] ?? { total: 0, workflow: 0 };
    if (counts.total >= Math.max(1, limits.queueMax - limits.internalReservedSlots)) {
      throw new ReckonWebhookError(429, { error: "queue_full" }, 10);
    }
    if (counts.workflow >= Math.max(1, limits.queueMaxPerWorkflow - 1)) {
      throw new ReckonWebhookError(429, { error: "workflow_queue_full" }, 10);
    }

    const runId = ulid(now.getTime());
    const workflowRunId = randomUUID();
    const workRunId = randomUUID();
    const threadId = randomUUID();
    const admissionId = randomUUID();
    const expiresAt = new Date(now.getTime() + access.rows[0].retention_hours * 3_600_000);
    const message = buildReckonSeedMessage({
      mode: input.mode,
      params: input.params,
      startedAt: now,
      batchChunkSize: input.webhook.batchChunkSize,
    });
    const requestPayload = reckonSeedPayload({ mode: input.mode, params: input.params, message });

    await client.query(
      `insert into work_thread (id, org_id, title, channel, backend_state, created_at, updated_at, last_message_at)
       values ($1, $2, $3, 'workflow', '{}'::jsonb, $4, $4, $4)`,
      [threadId, input.webhook.orgId, `Webhook ${input.webhook.reckonWorkflowId}`, now],
    );
    await client.query(
      `insert into work_run (id, org_id, thread_id, backend, status, actor_user_id, actor_role, created_at, updated_at)
       values ($1, $2, $3, coalesce((select config->>'backend' from llm_provider_config where org_id = $2 and scope = 'agent'), 'ax'), 'queued', null, 'service', $4, $4)`,
      [workRunId, input.webhook.orgId, threadId, now],
    );
    await admitRunSpend(client, {
      orgId: input.webhook.orgId,
      workflowId: input.webhook.workflowId,
      workRunId,
      source: "webhook",
      now,
    });
    await client.query(
      `insert into workflow_run (
         id, org_id, workflow_id, thread_id, work_run_id, trigger_kind, trigger_payload,
         execution_mode, trigger_input_preview, status, progress, admitted_at,
         result_expires_at, created_at, updated_at
       ) values ($1, $2, $3, $4, $5, 'api', $6::jsonb, 'single', $7::jsonb, 'queued', '{"stage":"queued"}'::jsonb, $8, $9, $8, $8)`,
      [
        workflowRunId,
        input.webhook.orgId,
        input.webhook.workflowId,
        threadId,
        workRunId,
        JSON.stringify({ source: "reckon_webhook", reckonWorkflowId: input.webhook.reckonWorkflowId, mode: input.mode }),
        JSON.stringify({ mode: input.mode, params: input.params }),
        now,
        expiresAt,
      ],
    );
    await client.query(
      `insert into workflow_api_admission (
         id, org_id, workflow_id, workflow_run_id, idempotency_hash, payload_hash,
         execution_mode, request_payload, request_bytes, status,
         reserved_tokens, reserved_cost_micros, available_at, created_at, updated_at, expires_at
       ) values ($1, $2, $3, $4, $5, $6, 'single', $7::jsonb, $8, 'pending', 0, 0, $9, $9, $9, $10)`,
      [
        admissionId,
        input.webhook.orgId,
        input.webhook.workflowId,
        workflowRunId,
        sha256Hex(`reckon:${input.webhook.reckonWorkflowId}:${input.idempotencyKey}`),
        fingerprint,
        JSON.stringify(requestPayload),
        input.requestBytes,
        now,
        expiresAt,
      ],
    );
    await client.query(
      `insert into compat_webhook_run (
         id, org_id, reckon_workflow_id, workflow_run_id, work_run_id,
         execution_mode, idempotency_key, request_fingerprint, created_at
       ) values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
      [
        runId,
        input.webhook.orgId,
        input.webhook.reckonWorkflowId,
        workflowRunId,
        workRunId,
        input.mode,
        input.idempotencyKey,
        fingerprint,
        now,
      ],
    );
    await client.query("commit");
    return {
      runId,
      status: "queued",
      mode: input.mode,
      replay: false,
      immediatelyEligible: counts.total < limits.maxConcurrency,
    };
  } catch (error) {
    await client.query("rollback").catch(() => undefined);
    client.release();
    released = true;
    if (error instanceof SpendBudgetExceeded) {
      await recordBudgetBlocked(error);
      throw budgetRejection(error);
    }
    throw error;
  } finally {
    if (!released) client.release();
  }
}
