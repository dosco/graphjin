/**
 * Shared pg-boss job queue.
 *
 * Both web (enqueue) and worker (consume) import from here so queue names
 * and the boss singleton are aligned. Backed by the same Postgres as the
 * Drizzle client; pg-boss auto-creates its `pgboss.*` schema on first start.
 *
 * Connection comes from buildPoolConfig() (see ./connection.ts).
 */

import PgBoss from "pg-boss";
import { buildPoolConfig } from "./connection";

export const QUEUE = {
  BUSINESS_PROFILE_BUILD: "business_profile_build",
  INDUSTRY_INSIGHTS_BUILD: "industry_insights_build",
  BOOTSTRAP_METRICS_BUILD: "bootstrap_metrics_build",
  METRIC_REFRESH: "metric_refresh",
  METRIC_REFRESH_SCHEDULED_SWEEP: "metric_refresh_scheduled_sweep",
  WORK_RUN: "work_run",
  WORKFLOW_CRON_SWEEP: "workflow_cron_sweep",
  WORKFLOW_RUN_FIRE: "workflow_run_fire",
  WORKFLOW_OUTPUT_TTL_SWEEP: "workflow_output_ttl_sweep",
  BOT_NAME_SWEEP: "bot_name_sweep",
  ACTION_EXECUTE: "action_execute",
  CHANNEL_DELIVER: "channel_deliver",
  EMBEDDING_INDEX: "embedding_index",
  EMBEDDING_SWEEP: "embedding_sweep",
  SKILL_LEARN: "skill_learn",
  DIRECTORY_SYNC: "directory_sync",
} as const;

export type QueueName = (typeof QUEUE)[keyof typeof QUEUE];

/**
 * Queues of removed features. The worker deletes them at boot, which also
 * drops their pending jobs and recurring schedules on upgraded installs.
 */
export const RETIRED_QUEUES = [
  "records_import",
  "records_identity_link",
  "records_salesforce_export",
  "records_salesforce_sync",
  "records_salesforce_sync_sweep",
  "records_salesforce_cutover",
  "records_backup_verify",
  "records_ops_watch",
  "records_watch_evaluate",
  "records_watch_sweep",
] as const;

export type ProcessingJobPayload = {
  queuedAt?: number;
  processingJobId: string;
  orgId: string;
};

export type MetricRefreshPayload = ProcessingJobPayload & {
  trigger?: string;
};

export type WorkRunPayload = ProcessingJobPayload & {
  queuedAt?: number;
  /** work_run.id — the row the worker will update. */
  runId: string;
  /** work_thread.id this run belongs to. */
  threadId: string;
  /** The user message that kicked off this run. */
  message: string;
  /** Delivery channel ("web", "telegram", …). Omitted ⇒ "web". Gates a2ui
   *  rendering — see docs/PER_CHANNEL_RENDERING.md. */
  channel?: string;
  /** For channel-initiated runs: the channel plugin + sender to deliver the
   *  reply back to. Absent for web runs (the UI streams over SSE). */
  channelPlugin?: string;
  recipient?: Record<string, unknown>;
  /** CH1: channel-native identity of the human who sent the message
   *  (Telegram from.id, Slack event.user+team). K1 resolves
   *  it to an actor; CH3 links it to an app_user. */
  sender?: { id: string; displayName?: string; workspaceId?: string };
};

/**
 * A durable, retryable outbound channel delivery. Enqueued (not delivered
 * inline) so a transient network failure or a worker restart can't drop the
 * message — pg-boss retries with backoff and the job survives restarts.
 */
export type ChannelDeliverPayload = {
  orgId: string;
  channelPlugin: string;
  recipient: Record<string, unknown>;
  /** InteractionEvent[] to deliver (serialized JSON). */
  events: unknown[];
};

export type WorkflowRunFirePayload = {
  queuedAt?: number;
  orgId: string;
  workflowId: string;
  triggerKind: "manual" | "cron" | "subscription" | "watcher" | "api";
  /** Durable cron firing ledger id. Cron deliveries use this as their
   * consumer-side idempotency boundary before creating a workflow run. */
  scheduleFiringId?: string;
  triggerPayload?: Record<string, unknown>;
  userMessage?: string;
  threadId?: string;
  parentChainDepth?: number;
  triggeredBySubscriptionId?: string;
  triggeredByOutputId?: string;
  triggeredByObservationId?: string;
  /** External API runs are pre-created transactionally by admission. */
  apiAdmissionId?: string;
  workflowRunId?: string;
  workRunId?: string;
  executionMode?: "single" | "batch";
  admittedAt?: string;
  queueAttempt?: number;
};

export type ActionExecutePayload = {
  orgId: string;
  actionRequestId: string;
};

export type SkillLearnPayload = {
  orgId: string;
  skillName: string;
};

let _boss: PgBoss | null = null;
let _starting: Promise<PgBoss> | null = null;

/**
 * Returns a started pg-boss instance, creating the singleton on first call.
 * Concurrent callers all await the same start so we never start twice.
 */
export async function boss(): Promise<PgBoss> {
  if (_boss) return _boss;
  if (_starting) return _starting;
  _starting = (async () => {
    const cfg = buildPoolConfig();
    const instance = new PgBoss({
      host: cfg.host,
      port: cfg.port,
      user: cfg.user,
      password: typeof cfg.password === "string" ? cfg.password : undefined,
      database: cfg.database,
      ssl: cfg.ssl,
      // Keep retention conservative for a CXO briefing tool — failed jobs
      // stay 7 days for forensic value, completed jobs purge sooner.
      retentionDays: 7,
    });
    instance.on("error", (e) => console.error("[pg-boss] error:", e));
    await instance.start();
    _boss = instance;
    return instance;
  })();
  return _starting;
}

// Retry policy is hardcoded; if we ever need it tunable, surface as a
// scope='agent' / scope='worker' setting and read from DB at boot.
const DEFAULT_SEND_OPTS: PgBoss.SendOptions = {
  retryLimit: 2,
  retryDelay: 30,
};

// pg-boss v10 stopped auto-creating queues on send() — silently returns null
// if the queue doesn't exist yet. Track which queues we've created so the
// first enqueue per queue name implicitly provisions it.
const _ensuredQueues = new Set<string>();

async function ensureQueue(name: string): Promise<void> {
  if (_ensuredQueues.has(name)) return;
  const b = await boss();
  await b.createQueue(name);
  _ensuredQueues.add(name);
}

/**
 * Enqueue a job by queue name. Web routes call this after writing the
 * processing_job row that backs the UI progress feed; worker handlers
 * call it to chain follow-up jobs. Default send options apply our
 * retry policy; callers can override per-call.
 */
export async function enqueue<T extends object>(
  queue: QueueName,
  data: T,
  opts?: PgBoss.SendOptions,
): Promise<string | null> {
  await ensureQueue(queue);
  const b = await boss();
  return b.send(queue, (queue === QUEUE.WORK_RUN || queue === QUEUE.METRIC_REFRESH || queue === QUEUE.WORKFLOW_RUN_FIRE) ? { ...(data as object), queuedAt: Date.now() } : data as object, { ...DEFAULT_SEND_OPTS, ...(opts ?? {}) });
}

export async function stopBoss(): Promise<void> {
  if (_boss) {
    await _boss.stop({ graceful: true });
    _boss = null;
    _starting = null;
    _ensuredQueues.clear();
  }
}
