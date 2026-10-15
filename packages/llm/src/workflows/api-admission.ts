import { startupPhase } from "@neko/telemetry/startup";
import { randomUUID } from "node:crypto";
import {
  mkdir,
  readFile,
  realpath,
  stat,
  unlink,
  writeFile,
} from "node:fs/promises";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { pool } from "@neko/db";
import type { HarnessRunSummary } from "@neko/telemetry";
import type { PoolClient } from "pg";
import { admitRunSpend, recordBudgetBlocked, SpendBudgetExceeded } from "../spend/admission";
import { getOrgAgentRoot } from "../work/workspace";
import {
  WorkflowApiError,
  hashWorkflowApiIdempotencyKey,
  parseCompiledWorkflowBatchContract,
  validateWorkflowApiIdempotencyKey,
  validateWorkflowApiInput,
  verifyWorkflowApiTokenDigest,
  type CompiledWorkflowBatchContract,
  type WorkflowApiExecutionMode,
  type WorkflowApiLimits,
} from "./api-contract";
import {
  verifyWorkflowApiAccessToken,
  type VerifiedWorkflowApiAccess,
} from "./api-access";
import { recordAuditEvent } from "./audit-chain";

const API_ACTIVE_STATUSES = [
  "pending",
  "dispatching",
  "enqueued",
  "running",
] as const;
const API_TERMINAL_RUN_STATUSES = new Set([
  "completed",
  "failed",
  "cancelled",
  "needs_input",
]);
const DEFAULT_DISPATCH_LEASE_MS = 2 * 60_000;
const DEFAULT_GLOBAL_QUEUE_CAP = 100;
const DEFAULT_ORG_QUEUE_CAP = 50;
const DEFAULT_GLOBAL_CONCURRENCY_CAP = 3;
const DEFAULT_ORG_CONCURRENCY_CAP = 3;
const DEFAULT_ORG_ROLLING_TOKEN_BUDGET = 1_000_000;
const DEFAULT_ORG_ROLLING_COST_MICROS_BUDGET = 50_000_000;

function boundedEnvironmentInteger(
  name: string,
  fallback: number,
  minimum: number,
  maximum: number,
): number {
  const parsed = Number.parseInt(process.env[name] ?? "", 10);
  return Number.isSafeInteger(parsed)
    ? Math.max(minimum, Math.min(maximum, parsed))
    : fallback;
}

function deploymentLimits() {
  return {
    globalQueueCap: boundedEnvironmentInteger(
      "OPENNEKO_WORKFLOW_API_GLOBAL_QUEUE_CAP",
      DEFAULT_GLOBAL_QUEUE_CAP,
      1,
      1_000,
    ),
    orgQueueCap: boundedEnvironmentInteger(
      "OPENNEKO_WORKFLOW_API_ORG_QUEUE_CAP",
      DEFAULT_ORG_QUEUE_CAP,
      1,
      500,
    ),
    globalConcurrencyCap: boundedEnvironmentInteger(
      "OPENNEKO_WORKFLOW_API_GLOBAL_CONCURRENCY_CAP",
      DEFAULT_GLOBAL_CONCURRENCY_CAP,
      1,
      50,
    ),
    orgConcurrencyCap: boundedEnvironmentInteger(
      "OPENNEKO_WORKFLOW_API_ORG_CONCURRENCY_CAP",
      DEFAULT_ORG_CONCURRENCY_CAP,
      1,
      50,
    ),
    orgRequestLimitPerMinute: boundedEnvironmentInteger(
      "OPENNEKO_WORKFLOW_API_ORG_REQUESTS_PER_MINUTE",
      120,
      1,
      2_400,
    ),
    orgPollLimitPerMinute: boundedEnvironmentInteger(
      "OPENNEKO_WORKFLOW_API_ORG_POLLS_PER_MINUTE",
      600,
      1,
      4_800,
    ),
    orgRollingTokenBudget: boundedEnvironmentInteger(
      "OPENNEKO_WORKFLOW_API_ORG_ROLLING_TOKEN_BUDGET",
      DEFAULT_ORG_ROLLING_TOKEN_BUDGET,
      1_000,
      100_000_000,
    ),
    orgRollingCostMicrosBudget: boundedEnvironmentInteger(
      "OPENNEKO_WORKFLOW_API_ORG_ROLLING_COST_MICROS_BUDGET",
      DEFAULT_ORG_ROLLING_COST_MICROS_BUDGET,
      1_000,
      10_000_000_000,
    ),
  };
}

type LockedAccessRow = {
  org_id: string;
  workflow_id: string;
  enabled: boolean;
  token_verifier: string | null;
  request_limit_per_minute: number;
  poll_limit_per_minute: number;
  queue_cap: number;
  concurrency_cap: number;
  batch_max_records: number;
  batch_chunk_size: number;
  max_request_bytes: number;
  max_result_bytes: number;
  max_artifact_bytes: number;
  max_runtime_seconds: number;
  max_model_calls: number;
  max_tool_calls: number;
  max_tokens_per_run: number;
  max_cost_micros_per_run: number;
  rolling_window_seconds: number;
  rolling_token_budget: number;
  rolling_cost_micros_budget: number;
  retention_hours: number;
  workflow_name: string;
  workflow_enabled: boolean;
  daily_run_budget: number | null;
  output_contract: Record<string, unknown> | null;
};

function lockedLimits(row: LockedAccessRow): WorkflowApiLimits {
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

async function lockWorkflowApiAccess(
  client: PoolClient,
  workflowId: string,
): Promise<LockedAccessRow | null> {
  const result = await client.query<LockedAccessRow>(
    `select access.*,
            workflow.name as workflow_name,
            workflow.enabled as workflow_enabled,
            workflow.daily_run_budget,
            workflow.output_contract
     from workflow_api_access access
     join workflow_definition workflow
       on workflow.id = access.workflow_id
      and workflow.org_id = access.org_id
     where access.workflow_id = $1
     for update of access, workflow`,
    [workflowId],
  );
  return result.rows[0] ?? null;
}

function retryAfterForMinute(now: Date): number {
  return Math.max(1, 60 - now.getUTCSeconds());
}

async function consumeRateBucket(
  client: PoolClient,
  input: {
    scopeKind: string;
    scopeId: string;
    operation: string;
    limit: number;
    now: Date;
  },
): Promise<void> {
  const result = await client.query(
    `insert into workflow_api_rate_bucket (
       scope_kind, scope_id, operation, window_start, count, expires_at
     ) values (
       $1, $2, $3, date_trunc('minute', $4::timestamptz), 1,
       date_trunc('minute', $4::timestamptz) + interval '2 minutes'
     )
     on conflict (scope_kind, scope_id, operation, window_start)
     do update set count = workflow_api_rate_bucket.count + 1
       where workflow_api_rate_bucket.count < $5
     returning count`,
    [input.scopeKind, input.scopeId, input.operation, input.now, input.limit],
  );
  if ((result.rowCount ?? 0) === 0) {
    throw new WorkflowApiError(
      `${input.operation}_rate_limited`,
      "The request rate limit has been reached.",
      429,
      retryAfterForMinute(input.now),
    );
  }
}

/**
 * Shared edge throttle, including malformed and invalid credentials. Scope IDs
 * are keyed HMACs, so raw network addresses never become stored/logged labels.
 */
export async function enforceWorkflowApiEdgeThrottle(
  clientFingerprint: string,
  now = new Date(),
): Promise<void> {
  const client = await pool().connect();
  try {
    await client.query("BEGIN");
    await consumeRateBucket(client, {
      scopeKind: "deployment",
      scopeId: "external-workflow-api",
      operation: "edge",
      limit: 1_000,
      now,
    });
    await consumeRateBucket(client, {
      scopeKind: "client",
      scopeId: clientFingerprint,
      operation: "edge",
      limit: 60,
      now,
    });
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK").catch(() => undefined);
    throw error;
  } finally {
    client.release();
  }
}

function batchInputRelativePath(workRunId: string): string {
  return join("runs", workRunId, "api-batch-input.ndjson");
}

export function resolveWorkflowApiWorkspacePath(
  orgId: string,
  relativePath: string,
): string {
  const root = resolve(getOrgAgentRoot(orgId));
  const candidate = resolve(root, relativePath);
  if (candidate !== root && !candidate.startsWith(`${root}${sep}`)) {
    throw new Error("Workflow API file path escaped the organization workspace.");
  }
  return candidate;
}

function isWithin(root: string, candidate: string): boolean {
  const rel = relative(root, candidate);
  return rel === "" || (!rel.startsWith("..") && !isAbsolute(rel));
}

/** Resolve through symlinks and accept only files physically contained by the
 * organization workspace. Callers translate null into their purpose-specific
 * unavailable response without revealing filesystem details. */
async function resolveWorkflowApiPhysicalFile(
  orgId: string,
  relativePath: string,
): Promise<string | null> {
  const declaredRoot = resolve(getOrgAgentRoot(orgId));
  const declaredFile = resolveWorkflowApiWorkspacePath(orgId, relativePath);
  const [physicalRoot, physicalFile] = await Promise.all([
    realpath(declaredRoot).catch(() => null),
    realpath(declaredFile).catch(() => null),
  ]);
  if (!physicalRoot || !physicalFile || !isWithin(physicalRoot, physicalFile)) {
    return null;
  }
  return physicalFile;
}

async function stageBatchInput(input: {
  orgId: string;
  workRunId: string;
  records: Record<string, unknown>[];
}): Promise<string> {
  const relativePath = batchInputRelativePath(input.workRunId);
  const absolutePath = resolveWorkflowApiWorkspacePath(input.orgId, relativePath);
  await mkdir(dirname(absolutePath), { recursive: true, mode: 0o700 });
  const body = input.records.map((record) => JSON.stringify(record)).join("\n");
  await writeFile(absolutePath, body ? `${body}\n` : "", {
    encoding: "utf8",
    flag: "wx",
    mode: 0o600,
  });
  return relativePath;
}

async function cleanupStagedFile(
  orgId: string,
  relativePath: string | null,
): Promise<void> {
  if (!relativePath) return;
  await unlink(resolveWorkflowApiWorkspacePath(orgId, relativePath)).catch(
    () => undefined,
  );
}

const SERIALIZABLE_ATTEMPTS = 8;

export async function withSerializableTransaction<T>(
  fn: (client: PoolClient) => Promise<T>,
  connect: () => Promise<PoolClient> = () => pool().connect(),
): Promise<T> {
  for (let attempt = 1; ; attempt += 1) {
    const client = await connect();
    let conflict = false;
    try {
      await client.query("BEGIN ISOLATION LEVEL SERIALIZABLE");
      const result = await fn(client);
      await client.query("COMMIT");
      return result;
    } catch (error) {
      await client.query("ROLLBACK").catch(() => undefined);
      if ((error as { code?: string } | null)?.code !== "40001") throw error;
      conflict = true;
    } finally {
      client.release();
    }
    if (conflict && attempt >= SERIALIZABLE_ATTEMPTS) {
      throw new WorkflowApiError(
        "busy",
        "The workflow API is busy. Retry the request with the same Idempotency-Key.",
        503,
        1,
      );
    }
    await new Promise((done) => setTimeout(done, Math.random() * 25 * 2 ** Math.min(attempt, 5)));
  }
}

type ExistingAdmissionRow = {
  workflow_run_id: string;
  payload_hash: string;
  execution_mode: WorkflowApiExecutionMode;
  status: string;
  run_status: string;
};

export type WorkflowApiAdmissionResult = {
  runId: string;
  status: string;
  mode: WorkflowApiExecutionMode;
  replay: boolean;
  statusUrl: string;
  expiresAt: Date;
};

async function checkQueueAndBudgets(
  client: PoolClient,
  input: {
    access: LockedAccessRow;
    limits: WorkflowApiLimits;
    mode: WorkflowApiExecutionMode;
    now: Date;
  },
): Promise<{ reservedTokens: number; reservedCostMicros: number }> {
  const deployment = deploymentLimits();
  const orgLimits = await client.query<{
    rolling_token_budget: number;
    rolling_cost_micros_budget: string;
  }>(
    "select rolling_token_budget, rolling_cost_micros_budget from workflow_api_org_limits where org_id = $1",
    [input.access.org_id],
  );
  const orgTokenBudget = orgLimits.rows[0]?.rolling_token_budget ?? deployment.orgRollingTokenBudget;
  const orgCostBudget = Number(orgLimits.rows[0]?.rolling_cost_micros_budget ?? deployment.orgRollingCostMicrosBudget);
  const counts = await client.query<{
    global_count: string;
    org_count: string;
    workflow_count: string;
  }>(
    `select
       count(*) filter (where status = any($1::text[])) as global_count,
       count(*) filter (where org_id = $2 and status = any($1::text[])) as org_count,
       count(*) filter (where workflow_id = $3 and status = any($1::text[])) as workflow_count
     from workflow_api_admission`,
    [API_ACTIVE_STATUSES, input.access.org_id, input.access.workflow_id],
  );
  const row = counts.rows[0];
  if (
    Number(row?.global_count ?? 0) >= deployment.globalQueueCap ||
    Number(row?.org_count ?? 0) >= deployment.orgQueueCap ||
    Number(row?.workflow_count ?? 0) >= input.limits.queueCap
  ) {
    throw new WorkflowApiError(
      "queue_full",
      "The workflow API queue is at capacity.",
      429,
      15,
    );
  }

  if (input.access.daily_run_budget !== null) {
    const daily = await client.query<{ count: string }>(
      `select count(*) as count
       from workflow_run
       where workflow_id = $1
         and created_at >= date_trunc('day', now() at time zone 'UTC') at time zone 'UTC'`,
      [input.access.workflow_id],
    );
    if (Number(daily.rows[0]?.count ?? 0) >= input.access.daily_run_budget) {
      throw new WorkflowApiError(
        "daily_run_budget_exhausted",
        "The workflow's daily run budget has been exhausted.",
        429,
        60 * 60,
      );
    }
  }

  const reservedTokens =
    input.mode === "batch" ? 0 : input.limits.maxTokensPerRun;
  const reservedCostMicros =
    input.mode === "batch" ? 0 : input.limits.maxCostMicrosPerRun;
  const usage = await client.query<{
    workflow_cost: string;
    org_tokens: string;
    org_cost: string;
  }>(
    `select
       coalesce(sum(coalesce(actual_cost_micros, 0)) filter (
         where workflow_id = $1
           and created_at >=
             $3::timestamptz - ($4::bigint * interval '1 second')
       ), 0) as workflow_cost,
       coalesce(sum(coalesce(actual_tokens, 0)) filter (
         where org_id = $2
           and created_at >=
             $3::timestamptz - ($4::bigint * interval '1 second')
       ), 0) as org_tokens,
       coalesce(sum(coalesce(actual_cost_micros, 0)) filter (
         where org_id = $2
           and created_at >=
             $3::timestamptz - ($4::bigint * interval '1 second')
       ), 0) as org_cost
     from workflow_api_admission
     where status <> 'expired'`,
    [
      input.access.workflow_id,
      input.access.org_id,
      input.now,
      input.limits.rollingWindowSeconds,
    ],
  );
  // A routine's budget is its cost; the organization also caps tokens. Each run still stops at its own ceilings.
  const totals = usage.rows[0];
  if (
    Number(totals?.workflow_cost ?? 0) >= input.limits.rollingCostMicrosBudget ||
    Number(totals?.org_tokens ?? 0) >= orgTokenBudget ||
    Number(totals?.org_cost ?? 0) >= orgCostBudget
  ) {
    throw new WorkflowApiError(
      "rolling_budget_exhausted",
      "The rolling workflow API usage budget has been exhausted.",
      429,
      Math.min(input.limits.rollingWindowSeconds, 3_600),
    );
  }
  return { reservedTokens, reservedCostMicros };
}

export async function admitWorkflowApiRun(input: {
  workflowId: string;
  token: string;
  idempotencyKey: string;
  mode: WorkflowApiExecutionMode;
  value: unknown;
  clientFingerprint: string;
  now?: Date;
}): Promise<WorkflowApiAdmissionResult> {
  const now = input.now ?? new Date();
  const idempotencyKey = validateWorkflowApiIdempotencyKey(input.idempotencyKey);
  const verified = await startupPhase("workflow.api_authenticate", async () => verifyWorkflowApiAccessToken(
    input.workflowId,
    input.token,
  ));
  if (!verified) {
    throw new WorkflowApiError(
      "invalid_credentials",
      "The workflow credential is invalid.",
      401,
    );
  }
  const initialValidation = validateWorkflowApiInput({
    value: input.value,
    mode: input.mode,
    limits: verified.limits,
    batchContract: verified.batchContract,
  });
  const idempotencyHash = hashWorkflowApiIdempotencyKey(
    input.workflowId,
    idempotencyKey,
  );
  const workflowRunId = randomUUID();
  const workRunId = randomUUID();
  const threadId = randomUUID();
  const admissionId = randomUUID();
  let stagedPath: string | null = null;
  if (input.mode === "batch") {
    stagedPath = await startupPhase("workflow.api_stage_input", async () => stageBatchInput({
      orgId: verified.orgId,
      workRunId,
      records: initialValidation.records ?? [],
    }));
  }

  let created = false;
  try {
    const result = await startupPhase("workflow.api_transaction", async () => withSerializableTransaction(async (client) => {
      const access = await lockWorkflowApiAccess(client, input.workflowId);
      const tokenMatches = verifyWorkflowApiTokenDigest(
        input.token,
        access?.token_verifier,
      );
      if (!access || !access.enabled || !tokenMatches) {
        throw new WorkflowApiError(
          "invalid_credentials",
          "The workflow credential is invalid.",
          401,
        );
      }
      if (!access.workflow_enabled) {
        throw new WorkflowApiError(
          "workflow_disabled",
          "The workflow is disabled.",
          409,
        );
      }
      const limits = lockedLimits(access);
      const batchContract = parseCompiledWorkflowBatchContract(
        access.output_contract,
      );
      const validation = validateWorkflowApiInput({
        value: input.value,
        mode: input.mode,
        limits,
        batchContract,
      });
      await consumeRateBucket(client, {
        scopeKind: "workflow",
        scopeId: access.workflow_id,
        operation: "invoke",
        limit: limits.requestLimitPerMinute,
        now,
      });
      await consumeRateBucket(client, {
        scopeKind: "organization",
        scopeId: access.org_id,
        operation: "invoke",
        limit: deploymentLimits().orgRequestLimitPerMinute,
        now,
      });
      await consumeRateBucket(client, {
        scopeKind: "client",
        scopeId: input.clientFingerprint,
        operation: "invoke",
        limit: Math.max(10, limits.requestLimitPerMinute * 2),
        now,
      });

      const existing = await client.query<ExistingAdmissionRow>(
        `select admission.workflow_run_id, admission.payload_hash,
                admission.execution_mode, admission.status,
                run.status as run_status
         from workflow_api_admission admission
         join workflow_run run on run.id = admission.workflow_run_id
         where admission.org_id = $1
           and admission.workflow_id = $2
           and admission.idempotency_hash = $3`,
        [access.org_id, access.workflow_id, idempotencyHash],
      );
      const replay = existing.rows[0];
      if (replay) {
        if (
          replay.payload_hash !== validation.payloadHash ||
          replay.execution_mode !== input.mode
        ) {
          throw new WorkflowApiError(
            "idempotency_conflict",
            "The Idempotency-Key was already used with different input.",
            409,
          );
        }
        const expires = await client.query<{ expires_at: Date }>(
          "select expires_at from workflow_api_admission where workflow_run_id = $1",
          [replay.workflow_run_id],
        );
        return {
          runId: replay.workflow_run_id,
          status: replay.run_status,
          mode: replay.execution_mode,
          replay: true,
          statusUrl: `/api/v1/workflows/${access.workflow_id}/runs/${replay.workflow_run_id}`,
          expiresAt: expires.rows[0]?.expires_at ?? now,
        };
      }

      const reservation = await checkQueueAndBudgets(client, {
        access,
        limits,
        mode: input.mode,
        now,
      });
      const expiresAt = new Date(
        now.getTime() + limits.retentionHours * 60 * 60 * 1_000,
      );
      const progress =
        input.mode === "batch"
          ? {
              stage: "queued",
              acceptedRows: validation.records?.length ?? 0,
              processedRows: 0,
              finalRows: 0,
              chunkCount: 0,
              plannedChunks: Math.ceil(
                (validation.records?.length ?? 0) / limits.batchChunkSize,
              ),
            }
          : { stage: "queued" };
      await client.query(
        `insert into work_thread (
           id, org_id, title, channel, backend_state,
           created_at, updated_at, last_message_at
         ) values ($1, $2, $3, 'workflow', '{}'::jsonb, $4, $4, $4)`,
        [threadId, access.org_id, access.workflow_name, now],
      );
      await client.query(
        `insert into work_run (
           id, org_id, thread_id, backend, status, actor_user_id, actor_role,
           created_at, updated_at
         ) values ($1, $2, $3, coalesce((select config->>'backend' from llm_provider_config where org_id = $2 and scope = 'agent'), 'hermes'), 'queued', null, 'service', $4, $4)`,
        [workRunId, access.org_id, threadId, now],
      );
      try {
        await admitRunSpend(client, {
          orgId: access.org_id,
          workflowId: access.workflow_id,
          workRunId,
          source: "api",
          now,
        });
      } catch (error) {
        if (error instanceof SpendBudgetExceeded) {
          setImmediate(() => void recordBudgetBlocked(error));
          throw new WorkflowApiError("spend_budget_exhausted", error.message, 429, error.retryAfterSeconds);
        }
        throw error;
      }
      await client.query(
        `insert into workflow_run (
           id, org_id, workflow_id, thread_id, work_run_id,
           trigger_kind, trigger_payload, execution_mode,
           trigger_input_preview, status, progress, admitted_at,
           result_expires_at, created_at, updated_at
         ) values (
           $1, $2, $3, $4, $5,
           'api', $6::jsonb, $7, $8::jsonb, 'queued', $9::jsonb, $10,
           $11, $10, $10
         )`,
        [
          workflowRunId,
          access.org_id,
          access.workflow_id,
          threadId,
          workRunId,
          JSON.stringify({
            source: "external_api",
            requestBytes: validation.bytes,
          }),
          input.mode,
          JSON.stringify(validation.preview),
          JSON.stringify(progress),
          now,
          expiresAt,
        ],
      );
      await client.query(
        `insert into workflow_api_admission (
           id, org_id, workflow_id, workflow_run_id,
           idempotency_hash, payload_hash, execution_mode,
           request_payload, input_file_path, batch_contract,
           request_bytes, accepted_records, status,
           reserved_tokens, reserved_cost_micros,
           available_at, created_at, updated_at, expires_at
         ) values (
           $1, $2, $3, $4,
           $5, $6, $7,
           $8::jsonb, $9, $10::jsonb,
           $11, $12, 'pending',
           $13, $14,
           $15, $15, $15, $16
         )`,
        [
          admissionId,
          access.org_id,
          access.workflow_id,
          workflowRunId,
          idempotencyHash,
          validation.payloadHash,
          input.mode,
          input.mode === "single" ? JSON.stringify(validation.input) : null,
          input.mode === "batch" ? stagedPath : null,
          input.mode === "batch" ? JSON.stringify(batchContract) : null,
          validation.bytes,
          input.mode === "batch" ? validation.records?.length ?? 0 : null,
          reservation.reservedTokens,
          reservation.reservedCostMicros,
          now,
          expiresAt,
        ],
      );
      await client.query(
        `update workflow_api_access
         set last_used_at = $2, updated_at = $2
         where workflow_id = $1`,
        [access.workflow_id, now],
      );
      created = true;
      return {
        runId: workflowRunId,
        status: "queued",
        mode: input.mode,
        replay: false,
        statusUrl: `/api/v1/workflows/${access.workflow_id}/runs/${workflowRunId}`,
        expiresAt,
      } satisfies WorkflowApiAdmissionResult;
    }));

    if (created) {
      await recordAuditEvent({
        orgId: verified.orgId,
        entityKind: "workflow_run",
        entityId: result.runId,
        event: "api_admitted",
        payload: {
          workflowId: input.workflowId,
          mode: input.mode,
          requestBytes: initialValidation.bytes,
          acceptedRecords: initialValidation.records?.length ?? null,
        },
      });
    } else {
      await cleanupStagedFile(verified.orgId, stagedPath);
    }
    return result;
  } catch (error) {
    if (!created) await cleanupStagedFile(verified.orgId, stagedPath);
    throw error;
  }
}

export type LeasedWorkflowApiAdmission = {
  id: string;
  orgId: string;
  workflowId: string;
  workflowRunId: string;
  workRunId: string;
  threadId: string;
  executionMode: WorkflowApiExecutionMode;
  admittedAt: Date;
  attempts: number;
};

export async function leasePendingWorkflowApiAdmissions(
  input: { now?: Date; limit?: number; leaseMs?: number } = {},
): Promise<LeasedWorkflowApiAdmission[]> {
  const now = input.now ?? new Date();
  const limit = Math.max(1, Math.min(input.limit ?? 50, 200));
  const leaseUntil = new Date(
    now.getTime() + (input.leaseMs ?? DEFAULT_DISPATCH_LEASE_MS),
  );
  const result = await pool().query<{
    id: string;
    org_id: string;
    workflow_id: string;
    workflow_run_id: string;
    work_run_id: string;
    thread_id: string;
    execution_mode: WorkflowApiExecutionMode;
    admitted_at: Date;
    attempts: number;
  }>(
    `with candidates as (
       select id
       from workflow_api_admission
       where expires_at > $1
         and (
           (status = 'pending' and available_at <= $1)
           or (status = 'dispatching' and lease_until <= $1)
           or (status = 'enqueued' and updated_at < $1 - interval '30 minutes')
         )
       order by available_at, created_at
       for update skip locked
       limit $2
     )
     update workflow_api_admission admission
     set status = 'dispatching', attempts = admission.attempts + 1,
         lease_until = $3, updated_at = $1
     from candidates, workflow_run run
     where admission.id = candidates.id
       and run.id = admission.workflow_run_id
     returning admission.id, admission.org_id, admission.workflow_id,
               admission.workflow_run_id, run.work_run_id, run.thread_id,
               admission.execution_mode, run.admitted_at, admission.attempts`,
    [now, limit, leaseUntil],
  );
  return result.rows.map((row) => ({
    id: row.id,
    orgId: row.org_id,
    workflowId: row.workflow_id,
    workflowRunId: row.workflow_run_id,
    workRunId: row.work_run_id,
    threadId: row.thread_id,
    executionMode: row.execution_mode,
    admittedAt: row.admitted_at,
    attempts: row.attempts,
  }));
}

export async function markWorkflowApiAdmissionEnqueued(
  admissionId: string,
  queueJobId: string,
  now = new Date(),
): Promise<void> {
  await pool().query(
    `update workflow_api_admission
     set status = 'enqueued', queue_job_id = $2, lease_until = null,
         dispatched_at = $3, last_error_code = null, updated_at = $3
     where id = $1 and status = 'dispatching'`,
    [admissionId, queueJobId, now],
  );
}

export async function releaseWorkflowApiAdmissionDispatch(
  admissionId: string,
  errorCode: string,
  now = new Date(),
): Promise<void> {
  await pool().query(
    `update workflow_api_admission
     set status = 'pending', lease_until = null,
         available_at = $3 + interval '15 seconds',
         last_error_code = $2, updated_at = $3
     where id = $1 and status = 'dispatching'`,
    [admissionId, errorCode.slice(0, 80), now],
  );
}

export type ClaimedWorkflowApiAdmission = {
  action: "claimed";
  id: string;
  orgId: string;
  workflowId: string;
  workflowRunId: string;
  workRunId: string;
  threadId: string;
  mode: WorkflowApiExecutionMode;
  requestPayload: Record<string, unknown> | null;
  inputFilePath: string | null;
  batchContract: CompiledWorkflowBatchContract | null;
  acceptedRecords: number | null;
  limits: WorkflowApiLimits;
  admittedAt: Date;
  attempt: number;
};

export type WorkflowApiAdmissionClaim =
  | ClaimedWorkflowApiAdmission
  | { action: "deferred"; retryAfterSeconds: number }
  | { action: "duplicate" };

type AdmissionClaimRow = LockedAccessRow & {
  admission_id: string;
  admission_status: string;
  workflow_run_id: string;
  work_run_id: string;
  thread_id: string;
  execution_mode: WorkflowApiExecutionMode;
  request_payload: Record<string, unknown> | null;
  input_file_path: string | null;
  batch_contract: CompiledWorkflowBatchContract | null;
  accepted_records: number | null;
  admitted_at: Date;
  attempts: number;
  expires_at: Date;
  run_status: string;
};

export async function claimWorkflowApiAdmission(input: {
  admissionId: string;
  workflowRunId: string;
  orgId: string;
  workflowId: string;
  now?: Date;
}): Promise<WorkflowApiAdmissionClaim> {
  const now = input.now ?? new Date();
  return withSerializableTransaction(async (client) => {
    const result = await client.query<AdmissionClaimRow>(
      `select access.*,
              workflow.name as workflow_name,
              workflow.enabled as workflow_enabled,
              workflow.daily_run_budget,
              workflow.output_contract,
              admission.id as admission_id,
              admission.status as admission_status,
              admission.workflow_run_id,
              run.work_run_id, run.thread_id, admission.execution_mode,
              admission.request_payload, admission.input_file_path,
              admission.batch_contract, admission.accepted_records,
              run.admitted_at,
              admission.attempts, admission.expires_at,
              run.status as run_status
       from workflow_api_admission admission
       join workflow_run run on run.id = admission.workflow_run_id
       join workflow_definition workflow on workflow.id = admission.workflow_id
       join workflow_api_access access on access.workflow_id = admission.workflow_id
       where admission.id = $1
         and admission.workflow_run_id = $2
         and admission.org_id = $3
         and admission.workflow_id = $4
       for update of admission, run`,
      [
        input.admissionId,
        input.workflowRunId,
        input.orgId,
        input.workflowId,
      ],
    );
    const row = result.rows[0];
    if (!row) return { action: "duplicate" };
    if (
      API_TERMINAL_RUN_STATUSES.has(row.run_status) ||
      ["completed", "failed", "cancelled", "expired"].includes(
        row.admission_status,
      ) ||
      row.admission_status === "running"
    ) {
      return { action: "duplicate" };
    }
    if (row.expires_at <= now) {
      await client.query(
        `update workflow_api_admission
         set status = 'expired', completed_at = $2, updated_at = $2
         where id = $1`,
        [row.admission_id, now],
      );
      await client.query(
        `update workflow_run
         set status = 'cancelled', error = 'Result retention elapsed before execution.',
             finished_at = $2, updated_at = $2
         where id = $1`,
        [row.workflow_run_id, now],
      );
      await client.query(
        `update work_run
         set status = 'cancelled', finished_at = $2, updated_at = $2
         where id = $1`,
        [row.work_run_id, now],
      );
      return { action: "duplicate" };
    }

    const deployment = deploymentLimits();
    const active = await client.query<{
      global_count: string;
      org_count: string;
      workflow_count: string;
    }>(
      `select
         count(*) filter (where status = 'running') as global_count,
         count(*) filter (where org_id = $1 and status = 'running') as org_count,
         count(*) filter (where workflow_id = $2 and status = 'running') as workflow_count
       from workflow_api_admission`,
      [row.org_id, row.workflow_id],
    );
    const counts = active.rows[0];
    if (
      Number(counts?.global_count ?? 0) >= deployment.globalConcurrencyCap ||
      Number(counts?.org_count ?? 0) >= deployment.orgConcurrencyCap ||
      Number(counts?.workflow_count ?? 0) >= row.concurrency_cap
    ) {
      await client.query(
        `update workflow_api_admission
         set status = 'pending', available_at = $2 + interval '5 seconds',
             lease_until = null, updated_at = $2,
             last_error_code = 'concurrency_full'
         where id = $1`,
        [row.admission_id, now],
      );
      return { action: "deferred", retryAfterSeconds: 5 };
    }

    const limits = lockedLimits(row);
    const leaseUntil = new Date(
      now.getTime() + (limits.maxRuntimeSeconds + 120) * 1_000,
    );
    await client.query(
      `update workflow_api_admission
       set status = 'running', started_at = coalesce(started_at, $2),
           lease_until = $3, updated_at = $2, last_error_code = null
       where id = $1`,
      [row.admission_id, now, leaseUntil],
    );
    await client.query(
      `update workflow_run
       set status = 'running', started_at = coalesce(started_at, $2),
           queue_attempts = $3, updated_at = $2,
           progress = coalesce(progress, '{}'::jsonb) || '{"stage":"running"}'::jsonb
       where id = $1`,
      [row.workflow_run_id, now, row.attempts],
    );
    return {
      action: "claimed",
      id: row.admission_id,
      orgId: row.org_id,
      workflowId: row.workflow_id,
      workflowRunId: row.workflow_run_id,
      workRunId: row.work_run_id,
      threadId: row.thread_id,
      mode: row.execution_mode,
      requestPayload: row.request_payload,
      inputFilePath: row.input_file_path,
      batchContract: row.batch_contract,
      acceptedRecords: row.accepted_records,
      limits,
      admittedAt: row.admitted_at,
      attempt: row.attempts,
    };
  });
}

function telemetryActuals(summary: HarnessRunSummary | null | undefined): {
  tokens: number | null;
  costMicros: number | null;
} {
  if (!summary || summary.usage.coverage === "unavailable") {
    return { tokens: null, costMicros: null };
  }
  const tokens = summary.usage.totalTokens;
  const cost = summary.usage.billedCostUsd ?? summary.usage.estimatedCostUsd;
  return {
    tokens: typeof tokens === "number" && Number.isFinite(tokens) ? Math.ceil(tokens) : null,
    costMicros:
      typeof cost === "number" && Number.isFinite(cost)
        ? Math.max(0, Math.ceil(cost * 1_000_000))
        : null,
  };
}

export async function persistWorkflowApiTelemetry(input: {
  admissionId: string;
  workflowRunId: string;
  summary: HarnessRunSummary;
  /** Usage the worker metered from usage events; used when the summary has none. */
  metered?: { tokens: number; costMicros: number };
}): Promise<void> {
  const reported = telemetryActuals(input.summary);
  const actual = {
    tokens: reported.tokens ?? input.metered?.tokens ?? null,
    costMicros: reported.costMicros ?? input.metered?.costMicros ?? null,
  };
  try {
    await withSerializableTransaction(async (client) => {
      await client.query(
        `update workflow_run
         set telemetry_summary = $2::jsonb, updated_at = now()
         where id = $1`,
        [input.workflowRunId, JSON.stringify(input.summary)],
      );
      await client.query(
        `update workflow_api_admission
         set actual_tokens = $2, actual_cost_micros = $3, updated_at = now()
         where id = $1`,
        [input.admissionId, actual.tokens, actual.costMicros],
      );
    });
  } catch {
    console.warn(
      `[workflow-api.telemetry] failed to persist summary for run ${input.workflowRunId}`,
    );
  }
}

export async function updateWorkflowApiRunProgress(input: {
  workflowRunId: string;
  progress: Record<string, unknown>;
}): Promise<void> {
  await pool().query(
    `update workflow_run
     set progress = $2::jsonb, updated_at = now()
     where id = $1 and trigger_kind = 'api'`,
    [input.workflowRunId, JSON.stringify(input.progress)],
  );
}

export async function finishWorkflowApiAdmission(input: {
  admissionId: string;
  workflowRunId: string;
  workRunId: string;
  status: "completed" | "failed" | "cancelled" | "needs_input";
  terminalResult?: Record<string, unknown> | null;
  summary?: string | null;
  artifactPath?: string | null;
  progress?: Record<string, unknown>;
  error?: string | null;
  errorCode?: string | null;
  now?: Date;
}): Promise<void> {
  const now = input.now ?? new Date();
  const boundedError = input.error?.slice(0, 4_000) ?? null;
  const admissionStatus =
    input.status === "completed" || input.status === "needs_input"
      ? "completed"
      : input.status;
  await withSerializableTransaction(async (client) => {
    await client.query(
      `update workflow_api_admission
       set status = $2, completed_at = $3, lease_until = null,
           last_error_code = $4, updated_at = $3
       where id = $1`,
      [
        input.admissionId,
        admissionStatus,
        now,
        input.errorCode?.slice(0, 80) ?? null,
      ],
    );
    await client.query(
      `update workflow_run
       set status = $2,
           terminal_result = $3::jsonb,
           result_artifact_path = $4,
           progress = coalesce($5::jsonb, progress),
           summary = coalesce($6, summary),
           error = coalesce($7, error),
           finished_at = coalesce(finished_at, $8),
           updated_at = $8
       where id = $1`,
      [
        input.workflowRunId,
        input.status,
        input.terminalResult ? JSON.stringify(input.terminalResult) : null,
        input.artifactPath ?? null,
        input.progress ? JSON.stringify(input.progress) : null,
        input.summary ?? null,
        boundedError,
        now,
      ],
    );
    await client.query(
      `update work_run
       set status = case when $2 = 'needs_input' then 'failed' else $2 end,
           error = coalesce($3, error),
           finished_at = coalesce(finished_at, $4), updated_at = $4
       where id = $1`,
      [input.workRunId, input.status, boundedError, now],
    );
  });
}

export type WorkflowApiRunStatus = {
  runId: string;
  workflowId: string;
  mode: WorkflowApiExecutionMode;
  status: string;
  createdAt: Date;
  admittedAt: Date;
  startedAt: Date | null;
  finishedAt: Date | null;
  expiresAt: Date;
  progress: Record<string, unknown>;
  /** Present only when the workflow's API access includes telemetry. */
  telemetry?: HarnessRunSummary | null;
  result: Record<string, unknown> | null;
  artifact: { url: string } | null;
  error: { code: string; message: string } | null;
  retryAfterSeconds: number | null;
};

async function authorizeWorkflowApiPoll(input: {
  workflowId: string;
  token: string;
  clientFingerprint: string;
  now: Date;
}): Promise<VerifiedWorkflowApiAccess> {
  const verified = await verifyWorkflowApiAccessToken(
    input.workflowId,
    input.token,
  );
  if (!verified) {
    throw new WorkflowApiError(
      "invalid_credentials",
      "The workflow credential is invalid.",
      401,
    );
  }
  await withSerializableTransaction(async (client) => {
    await consumeRateBucket(client, {
      scopeKind: "workflow",
      scopeId: verified.workflowId,
      operation: "poll",
      limit: verified.limits.pollLimitPerMinute,
      now: input.now,
    });
    await consumeRateBucket(client, {
      scopeKind: "organization",
      scopeId: verified.orgId,
      operation: "poll",
      limit: deploymentLimits().orgPollLimitPerMinute,
      now: input.now,
    });
    await consumeRateBucket(client, {
      scopeKind: "client",
      scopeId: input.clientFingerprint,
      operation: "poll",
      limit: Math.max(30, verified.limits.pollLimitPerMinute * 2),
      now: input.now,
    });
  });
  return verified;
}

type ApiRunStatusRow = {
  id: string;
  workflow_id: string;
  execution_mode: WorkflowApiExecutionMode;
  status: string;
  created_at: Date;
  admitted_at: Date;
  started_at: Date | null;
  finished_at: Date | null;
  result_expires_at: Date;
  progress: Record<string, unknown>;
  telemetry_summary: HarnessRunSummary | null;
  terminal_result: Record<string, unknown> | null;
  result_artifact_path: string | null;
  error: string | null;
  last_error_code: string | null;
};

export async function getWorkflowApiRunStatus(input: {
  workflowId: string;
  runId: string;
  token: string;
  clientFingerprint: string;
  now?: Date;
}): Promise<WorkflowApiRunStatus> {
  const now = input.now ?? new Date();
  const verified = await authorizeWorkflowApiPoll({ ...input, now });
  const result = await pool().query<ApiRunStatusRow>(
    `select run.id, run.workflow_id, run.execution_mode, run.status,
            run.created_at, run.admitted_at, run.started_at, run.finished_at,
            run.result_expires_at, run.progress, run.telemetry_summary,
            run.terminal_result, run.result_artifact_path, run.error,
            admission.last_error_code
     from workflow_run run
     join workflow_api_admission admission
       on admission.workflow_run_id = run.id
     where run.id = $1 and run.workflow_id = $2 and run.org_id = $3`,
    [input.runId, input.workflowId, verified.orgId],
  );
  const row = result.rows[0];
  if (!row) {
    throw new WorkflowApiError("run_not_found", "Workflow run not found.", 404);
  }
  if (row.result_expires_at <= now) {
    throw new WorkflowApiError(
      "result_expired",
      "The retained API result has expired.",
      410,
    );
  }
  const terminal = API_TERMINAL_RUN_STATUSES.has(row.status);
  return {
    runId: row.id,
    workflowId: row.workflow_id,
    mode: row.execution_mode,
    status: row.status,
    createdAt: row.created_at,
    admittedAt: row.admitted_at,
    startedAt: row.started_at,
    finishedAt: row.finished_at,
    expiresAt: row.result_expires_at,
    progress: row.progress ?? {},
    ...(verified.includeTelemetry ? { telemetry: row.telemetry_summary } : {}),
    result: terminal ? row.terminal_result : null,
    artifact:
      terminal && row.result_artifact_path
        ? {
            url: `/api/v1/workflows/${row.workflow_id}/runs/${row.id}/artifact`,
          }
        : null,
    error:
      terminal && (row.error || row.last_error_code)
        ? {
            code: row.last_error_code ?? "workflow_failed",
            message: row.error ?? "Workflow execution failed.",
          }
        : null,
    retryAfterSeconds: terminal ? null : 3,
  };
}

export type WorkflowApiArtifact = {
  absolutePath: string;
  fileName: string;
  contentType: string;
  bytes: number;
};

export async function getWorkflowApiArtifact(input: {
  workflowId: string;
  runId: string;
  token: string;
  clientFingerprint: string;
  now?: Date;
}): Promise<WorkflowApiArtifact> {
  const now = input.now ?? new Date();
  const verified = await authorizeWorkflowApiPoll({ ...input, now });
  const result = await pool().query<{
    result_artifact_path: string | null;
    result_expires_at: Date;
    status: string;
  }>(
    `select result_artifact_path, result_expires_at, status
     from workflow_run
     where id = $1 and workflow_id = $2 and org_id = $3`,
    [input.runId, input.workflowId, verified.orgId],
  );
  const row = result.rows[0];
  if (!row) {
    throw new WorkflowApiError("run_not_found", "Workflow run not found.", 404);
  }
  if (row.result_expires_at <= now) {
    throw new WorkflowApiError("result_expired", "The retained API result has expired.", 410);
  }
  if (row.status !== "completed" || !row.result_artifact_path) {
    throw new WorkflowApiError(
      "artifact_not_ready",
      "The workflow artifact is not available.",
      404,
    );
  }
  const absolutePath = await resolveWorkflowApiPhysicalFile(
    verified.orgId,
    row.result_artifact_path,
  );
  const info = absolutePath ? await stat(absolutePath).catch(() => null) : null;
  if (
    !absolutePath ||
    !info?.isFile() ||
    info.size > verified.limits.maxArtifactBytes
  ) {
    throw new WorkflowApiError(
      "artifact_unavailable",
      "The workflow artifact is unavailable.",
      410,
    );
  }
  return {
    absolutePath,
    fileName: `workflow-${input.runId}.csv`,
    contentType: "text/csv; charset=utf-8",
    bytes: info.size,
  };
}

/** Session-authenticated operator download; independent of the public token gate. */
export async function getWorkflowApiArtifactForOperator(input: {
  orgId: string;
  runId: string;
  now?: Date;
}): Promise<WorkflowApiArtifact> {
  const now = input.now ?? new Date();
  const result = await pool().query<{
    result_artifact_path: string | null;
    result_expires_at: Date | null;
    status: string;
    max_artifact_bytes: number;
  }>(
    `select run.result_artifact_path, run.result_expires_at, run.status,
            access.max_artifact_bytes
     from workflow_run run
     join workflow_api_access access
       on access.workflow_id = run.workflow_id
      and access.org_id = run.org_id
     where run.id = $1 and run.org_id = $2 and run.trigger_kind = 'api'`,
    [input.runId, input.orgId],
  );
  const row = result.rows[0];
  if (!row) {
    throw new WorkflowApiError("run_not_found", "Workflow run not found.", 404);
  }
  if (!row.result_expires_at || row.result_expires_at <= now) {
    throw new WorkflowApiError(
      "result_expired",
      "The retained API result has expired.",
      410,
    );
  }
  if (row.status !== "completed" || !row.result_artifact_path) {
    throw new WorkflowApiError(
      "artifact_not_ready",
      "The workflow artifact is not available.",
      404,
    );
  }
  const absolutePath = await resolveWorkflowApiPhysicalFile(
    input.orgId,
    row.result_artifact_path,
  );
  const info = absolutePath ? await stat(absolutePath).catch(() => null) : null;
  if (
    !absolutePath ||
    !info?.isFile() ||
    info.size > row.max_artifact_bytes
  ) {
    throw new WorkflowApiError(
      "artifact_unavailable",
      "The workflow artifact is unavailable.",
      410,
    );
  }
  return {
    absolutePath,
    fileName: `workflow-${input.runId}.csv`,
    contentType: "text/csv; charset=utf-8",
    bytes: info.size,
  };
}

export async function readWorkflowApiBatchInput(input: {
  orgId: string;
  relativePath: string;
  maxBytes: number;
}): Promise<Record<string, unknown>[]> {
  const absolutePath = await resolveWorkflowApiPhysicalFile(
    input.orgId,
    input.relativePath,
  );
  const info = absolutePath ? await stat(absolutePath).catch(() => null) : null;
  if (!absolutePath || !info?.isFile() || info.size > input.maxBytes) {
    throw new WorkflowApiError(
      "batch_input_unavailable",
      "The retained batch input is unavailable or exceeds its bound.",
      410,
    );
  }
  const content = await readFile(absolutePath, "utf8");
  if (!content.trim()) return [];
  return content
    .trimEnd()
    .split("\n")
    .map((line) => JSON.parse(line) as Record<string, unknown>);
}

export async function writeWorkflowApiResultArtifact(input: {
  orgId: string;
  workRunId: string;
  csv: string;
  maxBytes: number;
}): Promise<{ relativePath: string; bytes: number }> {
  const bytes = Buffer.byteLength(input.csv, "utf8");
  if (bytes > input.maxBytes) {
    throw new WorkflowApiError(
      "artifact_limit_exceeded",
      "The batch result exceeds this workflow's artifact limit.",
      413,
    );
  }
  const relativePath = join("runs", input.workRunId, "artifacts", "api-result.csv");
  const absolutePath = resolveWorkflowApiWorkspacePath(
    input.orgId,
    relativePath,
  );
  await mkdir(dirname(absolutePath), { recursive: true, mode: 0o700 });
  await writeFile(absolutePath, input.csv, {
    encoding: "utf8",
    flag: "wx",
    mode: 0o600,
  });
  return { relativePath, bytes };
}

export async function expireWorkflowApiResults(now = new Date()): Promise<number> {
  const expired = await pool().query<{
    id: string;
    org_id: string;
    input_file_path: string | null;
    result_artifact_path: string | null;
  }>(
    `select admission.id, admission.org_id, admission.input_file_path,
            run.result_artifact_path
     from workflow_api_admission admission
     join workflow_run run on run.id = admission.workflow_run_id
     where admission.expires_at <= $1 and admission.status <> 'expired'
     limit 200`,
    [now],
  );
  for (const row of expired.rows) {
    await cleanupStagedFile(row.org_id, row.input_file_path);
    await cleanupStagedFile(row.org_id, row.result_artifact_path);
    await pool().query(
      `with expired as (
         update workflow_api_admission
         set status = 'expired', request_payload = null, input_file_path = null,
             batch_contract = null, completed_at = coalesce(completed_at, $2),
             updated_at = $2
         where id = $1 and expires_at <= $2
         returning workflow_run_id
       )
       update workflow_run run
       set terminal_result = null, result_artifact_path = null,
           trigger_input_preview = null, updated_at = $2
       from expired where run.id = expired.workflow_run_id`,
      [row.id, now],
    );
  }
  await pool().query(
    "delete from workflow_api_rate_bucket where expires_at <= $1",
    [now],
  );
  return expired.rows.length;
}

/**
 * A worker crash after claiming a run must never start a second paid attempt.
 * Once its execution lease expires, close the canonical run with an explicit
 * interruption instead of replaying the model call and double-counting spend.
 */
export async function recoverStaleWorkflowApiAdmissions(
  now = new Date(),
): Promise<number> {
  const recovered = await pool().query(
    `with stale as (
       update workflow_api_admission
       set status = 'failed', completed_at = $1, lease_until = null,
           last_error_code = 'worker_interrupted', updated_at = $1
       where status = 'running' and lease_until <= $1
       returning workflow_run_id
     ), closed_runs as (
       update workflow_run run
       set status = 'failed', error = 'Worker interrupted before the run completed.',
           finished_at = $1, updated_at = $1
       from stale where run.id = stale.workflow_run_id
       returning run.work_run_id
     )
     update work_run work
     set status = 'failed', error = 'Worker interrupted before the run completed.',
         finished_at = $1, updated_at = $1
     from closed_runs where work.id = closed_runs.work_run_id
     returning work.id`,
    [now],
  );
  return recovered.rowCount ?? 0;
}

export function workflowApiRelativeArtifactPath(
  orgId: string,
  absolutePath: string,
): string {
  const root = resolve(getOrgAgentRoot(orgId));
  const rel = relative(root, resolve(absolutePath));
  if (!rel || rel.startsWith("..") || rel.includes(`${sep}..${sep}`)) {
    throw new Error("Artifact is outside the organization workspace.");
  }
  return rel;
}
