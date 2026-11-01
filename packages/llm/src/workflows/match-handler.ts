import { createHash } from "node:crypto";
import { enqueue as defaultEnqueue, QUEUE } from "@neko/db/jobs";
import { isWorkflowInAncestorChain as defaultIsCycle } from "./cycle-detection";
import {
  countSubscriptionsMatchingOutput as defaultCountSubsMatching,
  countWorkflowRunsForSubscription as defaultCountInFlight,
  countWorkflowRunsSince as defaultCountRunsSince,
  createObservation as defaultCreateObservation,
  getWorkflow as defaultGetWorkflow,
  hasRecentSourceWriteForWorkflow as defaultHasRecentSourceWrite,
  startOfTodayUtc,
  writeSourceChangeLog as defaultWriteSourceChangeLog,
  type SubscriptionRecord,
  type WorkflowRecord,
} from "./store";
import type {
  JsonScalar,
  SourceChangeMatch,
  WorkflowOutputMatch,
} from "./subscription-query";

export type MatchHandlerDecision =
  | { action: "enqueued"; observationId: string; jobId: string | null }
  | { action: "dropped"; reason: string };

export type HandleSubscriptionMatchOptions = {
  subscription: SubscriptionRecord;
  output: WorkflowOutputMatch;
  globalMaxChainDepth?: number;
  globalMaxFanoutPerOutput?: number;
  fanoutWindowMs?: number;
  /** Override for tests. */
  enqueue?: typeof defaultEnqueue;
  createObservation?: typeof defaultCreateObservation;
  countSubscriptionsMatchingOutput?: typeof defaultCountSubsMatching;
  countWorkflowRunsForSubscription?: typeof defaultCountInFlight;
  countWorkflowRunsSince?: typeof defaultCountRunsSince;
  getWorkflow?: typeof defaultGetWorkflow;
  isWorkflowInAncestorChain?: typeof defaultIsCycle;
  /** Read the chain depth of the producing run. Defaults to a real DB query. */
  resolveProducingRunChainDepth?: (
    workflowRunId: string,
  ) => Promise<number | null>;
};

export type HandleSourceChangeMatchOptions = {
  subscription: SubscriptionRecord;
  match: SourceChangeMatch;
  dataSourceId: string;
  fanoutWindowMs?: number;
  /** Override for tests. */
  enqueue?: typeof defaultEnqueue;
  createObservation?: typeof defaultCreateObservation;
  countWorkflowRunsForSubscription?: typeof defaultCountInFlight;
  countWorkflowRunsSince?: typeof defaultCountRunsSince;
  getWorkflow?: typeof defaultGetWorkflow;
  hasRecentSourceWriteForWorkflow?: typeof defaultHasRecentSourceWrite;
  writeSourceChangeLog?: typeof defaultWriteSourceChangeLog;
};

const DEFAULT_MAX_CHAIN_DEPTH = 20;
const DEFAULT_MAX_FANOUT_PER_OUTPUT = 32;
const DEFAULT_FANOUT_WINDOW_MS = 60_000;

/**
 * Handle a workflow_output subscription match: enforce loop-safety limits,
 * write a consumer-side observation row, then enqueue a WORKFLOW_RUN_FIRE
 * job for the consuming workflow.
 */
export async function handleSubscriptionMatch(
  opts: HandleSubscriptionMatchOptions,
): Promise<MatchHandlerDecision> {
  const enqueue = opts.enqueue ?? defaultEnqueue;
  const createObservation = opts.createObservation ?? defaultCreateObservation;
  const countSubsMatching =
    opts.countSubscriptionsMatchingOutput ?? defaultCountSubsMatching;
  const countInFlight =
    opts.countWorkflowRunsForSubscription ?? defaultCountInFlight;
  const countRunsSince =
    opts.countWorkflowRunsSince ?? defaultCountRunsSince;
  const getWorkflow = opts.getWorkflow ?? defaultGetWorkflow;
  const isCycle = opts.isWorkflowInAncestorChain ?? defaultIsCycle;
  const globalMaxChainDepth =
    opts.globalMaxChainDepth ?? DEFAULT_MAX_CHAIN_DEPTH;
  const globalMaxFanout =
    opts.globalMaxFanoutPerOutput ?? DEFAULT_MAX_FANOUT_PER_OUTPUT;
  const fanoutWindowMs = opts.fanoutWindowMs ?? DEFAULT_FANOUT_WINDOW_MS;

  if (opts.output.org_id !== opts.subscription.orgId) {
    return {
      action: "dropped",
      reason: `org mismatch (sub=${opts.subscription.orgId} output=${opts.output.org_id})`,
    };
  }

  const maxChain =
    opts.subscription.maxChainDepthOverride ?? globalMaxChainDepth;
  if (opts.resolveProducingRunChainDepth) {
    const parentDepth = await opts.resolveProducingRunChainDepth(
      opts.output.workflow_run_id,
    );
    if (parentDepth !== null && parentDepth + 1 > maxChain) {
      return {
        action: "dropped",
        reason: `chain depth ${parentDepth + 1} exceeds max ${maxChain}`,
      };
    }
  }

  const cycle = await isCycle(
    opts.output.workflow_run_id,
    opts.subscription.workflowId,
  );
  if (cycle) {
    return {
      action: "dropped",
      reason: `cycle detected — consumer workflow ${opts.subscription.workflowId} is already in the producing run's chain`,
    };
  }

  const fanout = await countSubsMatching(opts.output.id, fanoutWindowMs);
  if (fanout >= globalMaxFanout) {
    return {
      action: "dropped",
      reason: `fanout cap reached (${fanout}/${globalMaxFanout}) for output ${opts.output.id}`,
    };
  }

  const guard = await applyDeliveryGuards({
    subscription: opts.subscription,
    countInFlight,
    countRunsSince,
    getWorkflow,
  });
  if (!guard.allowed) return { action: "dropped", reason: guard.reason };

  const observationRow = await createObservation({
    orgId: opts.subscription.orgId,
    sourceOutputId: opts.output.id,
    consumerKind: "workflow",
    consumerWorkflowId: opts.subscription.workflowId,
    subscriptionId: opts.subscription.id,
    title: opts.output.title || null,
    mood: (opts.output.mood as "good" | "watch" | "act" | null) ?? null,
  });

  const singletonKey = buildWorkflowOutputIdempotencyKey(
    opts.subscription,
    opts.output,
  );

  const jobId = await enqueue(
    QUEUE.WORKFLOW_RUN_FIRE,
    {
      orgId: opts.subscription.orgId,
      workflowId: opts.subscription.workflowId,
      triggerKind: "subscription" as const,
      triggerPayload: {
        subscription_id: opts.subscription.id,
        output_id: opts.output.id,
        observation_id: observationRow.id,
      },
      triggeredBySubscriptionId: opts.subscription.id,
      triggeredByOutputId: opts.output.id,
      triggeredByObservationId: observationRow.id,
    },
    {
      singletonKey,
      singletonHours: 1,
    },
  );

  return {
    action: "enqueued",
    observationId: observationRow.id,
    jobId,
  };
}

/**
 * Handle a source_change subscription match: detect the responder-write cycle
 * via `workflow_run.source_writes`, write an audit row to source_change_log,
 * write a consumer-side observation (no source_output_id — the trigger came
 * from the operator's data, not an OpenNeko workflow), then enqueue
 * WORKFLOW_RUN_FIRE.
 */
export async function handleSourceChangeMatch(
  opts: HandleSourceChangeMatchOptions,
): Promise<MatchHandlerDecision> {
  const enqueue = opts.enqueue ?? defaultEnqueue;
  const createObservation = opts.createObservation ?? defaultCreateObservation;
  const countInFlight =
    opts.countWorkflowRunsForSubscription ?? defaultCountInFlight;
  const countRunsSince =
    opts.countWorkflowRunsSince ?? defaultCountRunsSince;
  const getWorkflow = opts.getWorkflow ?? defaultGetWorkflow;
  const hasRecentSourceWrite =
    opts.hasRecentSourceWriteForWorkflow ?? defaultHasRecentSourceWrite;
  const writeAudit = opts.writeSourceChangeLog ?? defaultWriteSourceChangeLog;
  const fanoutWindowMs = opts.fanoutWindowMs ?? DEFAULT_FANOUT_WINDOW_MS;

  const recentWrite = await hasRecentSourceWrite({
    workflowId: opts.subscription.workflowId,
    table: opts.match.table,
    primaryKey: opts.match.primary_key,
    sinceMs: fanoutWindowMs,
  });
  if (recentWrite) {
    return {
      action: "dropped",
      reason: `cycle detected — workflow ${opts.subscription.workflowId} recently wrote to ${opts.match.table}:${JSON.stringify(opts.match.primary_key)}`,
    };
  }

  const guard = await applyDeliveryGuards({
    subscription: opts.subscription,
    countInFlight,
    countRunsSince,
    getWorkflow,
  });
  if (!guard.allowed) return { action: "dropped", reason: guard.reason };

  const pkSummary = summarizePrimaryKey(opts.match.primary_key);
  const observationRow = await createObservation({
    orgId: opts.subscription.orgId,
    sourceOutputId: null,
    consumerKind: "workflow",
    consumerWorkflowId: opts.subscription.workflowId,
    subscriptionId: opts.subscription.id,
    title: `${opts.match.table} ${pkSummary}`,
    body: JSON.stringify(opts.match.snapshot).slice(0, 4_000),
    mood: null,
  });

  await writeAudit({
    orgId: opts.subscription.orgId,
    sourceId: opts.dataSourceId,
    tableName: opts.match.table,
    changeKind: "subscription_match",
    payload: {
      subscription_id: opts.subscription.id,
      observation_id: observationRow.id,
      primary_key: opts.match.primary_key,
      snapshot: opts.match.snapshot,
      version_token: opts.match.version_token,
    },
  });

  const singletonKey = buildSourceChangeIdempotencyKey(
    opts.subscription,
    opts.match,
  );

  const jobId = await enqueue(
    QUEUE.WORKFLOW_RUN_FIRE,
    {
      orgId: opts.subscription.orgId,
      workflowId: opts.subscription.workflowId,
      triggerKind: "subscription" as const,
      triggerPayload: {
        subscription_id: opts.subscription.id,
        observation_id: observationRow.id,
        table: opts.match.table,
        primary_key: opts.match.primary_key,
        snapshot: opts.match.snapshot,
        version_token: opts.match.version_token,
      },
      triggeredBySubscriptionId: opts.subscription.id,
      triggeredByObservationId: observationRow.id,
    },
    {
      singletonKey,
      singletonHours: 1,
    },
  );

  return {
    action: "enqueued",
    observationId: observationRow.id,
    jobId,
  };
}

export type ExternalEventMatch = {
  /** Event name, e.g. "invoice.paid". */
  name: string;
  /** Emitting system (plugin npm name, watcher id, "api", …). */
  source: string | null;
  payload: Record<string, unknown>;
  /** Caller-supplied dedupe key; defaults to a hash of name+source+payload. */
  dedupeKey?: string;
};

export type HandleExternalEventMatchOptions = {
  subscription: SubscriptionRecord;
  event: ExternalEventMatch;
  /** Override for tests. */
  enqueue?: typeof defaultEnqueue;
  createObservation?: typeof defaultCreateObservation;
  countWorkflowRunsForSubscription?: typeof defaultCountInFlight;
  countWorkflowRunsSince?: typeof defaultCountRunsSince;
  getWorkflow?: typeof defaultGetWorkflow;
};

/**
 * Handle an external_event subscription match: the trigger came from
 * outside the loop entirely (a watcher, a plugin webhook, the admin API),
 * so chain-depth/cycle/fanout don't apply — only the delivery guards
 * (consumer concurrency + daily budget). Writes a consumer-side
 * observation, then enqueues WORKFLOW_RUN_FIRE.
 */
export async function handleExternalEventMatch(
  opts: HandleExternalEventMatchOptions,
): Promise<MatchHandlerDecision> {
  const enqueue = opts.enqueue ?? defaultEnqueue;
  const createObservation = opts.createObservation ?? defaultCreateObservation;
  const countInFlight =
    opts.countWorkflowRunsForSubscription ?? defaultCountInFlight;
  const countRunsSince = opts.countWorkflowRunsSince ?? defaultCountRunsSince;
  const getWorkflow = opts.getWorkflow ?? defaultGetWorkflow;

  const guard = await applyDeliveryGuards({
    subscription: opts.subscription,
    countInFlight,
    countRunsSince,
    getWorkflow,
  });
  if (!guard.allowed) return { action: "dropped", reason: guard.reason };

  const observationRow = await createObservation({
    orgId: opts.subscription.orgId,
    consumerKind: "workflow",
    consumerWorkflowId: opts.subscription.workflowId,
    subscriptionId: opts.subscription.id,
    title: `${opts.event.source ?? "external"}: ${opts.event.name}`,
    body: JSON.stringify(opts.event.payload).slice(0, 4_000),
    mood: null,
  });

  const singletonKey = buildExternalEventIdempotencyKey(
    opts.subscription,
    opts.event,
  );

  const jobId = await enqueue(
    QUEUE.WORKFLOW_RUN_FIRE,
    {
      orgId: opts.subscription.orgId,
      workflowId: opts.subscription.workflowId,
      triggerKind: "subscription" as const,
      triggerPayload: {
        subscription_id: opts.subscription.id,
        observation_id: observationRow.id,
        event_name: opts.event.name,
        event_source: opts.event.source,
        payload: opts.event.payload,
      },
      triggeredBySubscriptionId: opts.subscription.id,
      triggeredByObservationId: observationRow.id,
    },
    {
      singletonKey,
      singletonHours: 1,
    },
  );

  return {
    action: "enqueued",
    observationId: observationRow.id,
    jobId,
  };
}

function buildExternalEventIdempotencyKey(
  sub: SubscriptionRecord,
  event: ExternalEventMatch,
): string {
  const eventKey =
    event.dedupeKey ??
    createHash("sha256")
      .update(`${event.name}\0${event.source ?? ""}\0${JSON.stringify(event.payload)}`)
      .digest("hex")
      .slice(0, 16);
  if (sub.idempotencyKeyTemplate) {
    return sub.idempotencyKeyTemplate
      .replace("{subscription_id}", sub.id)
      .replace("{source_record_id}", eventKey)
      .replace("{source_version}", "none");
  }
  return `${sub.id}:${eventKey}`;
}

type GuardArgs = {
  subscription: SubscriptionRecord;
  countInFlight: typeof defaultCountInFlight;
  countRunsSince: typeof defaultCountRunsSince;
  getWorkflow: typeof defaultGetWorkflow;
};

type GuardResult =
  | { allowed: true }
  | { allowed: false; reason: string };

async function applyDeliveryGuards(args: GuardArgs): Promise<GuardResult> {
  const inFlight = await args.countInFlight(args.subscription.id, "running");
  if (inFlight >= args.subscription.maxConcurrentRuns) {
    return {
      allowed: false,
      reason: `subscription ${args.subscription.id} at max_concurrent_runs (${inFlight}/${args.subscription.maxConcurrentRuns})`,
    };
  }
  const consumer: WorkflowRecord | null = await args.getWorkflow(
    args.subscription.orgId,
    args.subscription.workflowId,
  );
  if (consumer?.dailyRunBudget != null) {
    const ranToday = await args.countRunsSince(
      args.subscription.orgId,
      args.subscription.workflowId,
      startOfTodayUtc(),
    );
    if (ranToday >= consumer.dailyRunBudget) {
      return {
        allowed: false,
        reason: `consumer workflow ${args.subscription.workflowId} reached daily_run_budget (${ranToday}/${consumer.dailyRunBudget})`,
      };
    }
  }
  return { allowed: true };
}

function buildWorkflowOutputIdempotencyKey(
  sub: SubscriptionRecord,
  output: WorkflowOutputMatch,
): string {
  if (sub.idempotencyKeyTemplate) {
    return sub.idempotencyKeyTemplate
      .replace("{subscription_id}", sub.id)
      .replace("{source_record_id}", output.id)
      .replace("{source_version}", output.created_at);
  }
  return `${sub.id}:${output.id}:${output.created_at}`;
}

function buildSourceChangeIdempotencyKey(
  sub: SubscriptionRecord,
  match: SourceChangeMatch,
): string {
  const pkHash = hashPrimaryKey(match.primary_key);
  const versionToken = match.version_token ?? "none";
  if (sub.idempotencyKeyTemplate) {
    return sub.idempotencyKeyTemplate
      .replace("{subscription_id}", sub.id)
      .replace("{source_record_id}", pkHash)
      .replace("{primary_key}", pkHash)
      .replace("{source_version}", versionToken);
  }
  return `${sub.id}:${pkHash}:${versionToken}`;
}

function hashPrimaryKey(pk: Record<string, JsonScalar>): string {
  const parts = Object.entries(pk)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([k, v]) => `${k}=${v == null ? "" : String(v)}`);
  return createHash("sha256")
    .update(parts.join(""))
    .digest("hex")
    .slice(0, 16);
}

function summarizePrimaryKey(pk: Record<string, JsonScalar>): string {
  const parts = Object.entries(pk).map(([k, v]) => `${k}=${v == null ? "" : String(v)}`);
  return parts.length === 0 ? "()" : `(${parts.join(", ")})`;
}
