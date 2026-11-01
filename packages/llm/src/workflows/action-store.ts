import {
  ADMINISTRATORS_GROUP_SLUG,
  builtinGroupId,
  audit_chain,
  action_execution,
  action_policy,
  action_request,
  and,
  asc,
  db,
  desc,
  eq,
  pool,
  work_run,
} from "@neko/db";

import { resolveDeploymentProfile } from "../work/deployment-profile";

export type ActionScope = "internal" | "external";

export type ActionRequestStatus =
  | "draft"
  | "pending_approval"
  | "approved"
  | "rejected"
  | "expired"
  | "executed"
  | "failed"
  | "cancelled";

export type ActionExecutionStatus =
  | "pending"
  | "running"
  | "succeeded"
  | "reconcile_required"
  | "partially_applied"
  | "failed";

export type RiskLevel = "low" | "medium" | "high" | "critical";

export type ActionPolicyMode =
  | "observe_only"
  | "draft_only"
  | "auto_approve"
  | "approval_required"
  | "never";

export type ActionPolicyRecord = {
  id: string;
  orgId: string;
  name: string;
  description: string;
  appliesToKinds: string[];
  appliesToScopes: ActionScope[];
  mode: ActionPolicyMode;
  riskThresholdAutoApprove: RiskLevel | null;
  allowedTargets: Record<string, unknown> | null;
  deniedTargets: Record<string, unknown> | null;
  limits: Record<string, unknown>;
  /** Group whose members may approve; Administrators may always approve. */
  approverGroupId: string | null;
  priority: number;
  enabled: boolean;
  createdByThreadId: string | null;
  createdByRunId: string | null;
  createdAt: Date;
  updatedAt: Date;
};

function toPolicyRecord(
  row: typeof action_policy.$inferSelect,
): ActionPolicyRecord {
  return {
    id: row.id,
    orgId: row.org_id,
    name: row.name,
    description: row.description,
    appliesToKinds: row.applies_to_kinds,
    appliesToScopes: row.applies_to_scopes as ActionScope[],
    mode: row.mode as ActionPolicyMode,
    riskThresholdAutoApprove:
      (row.risk_threshold_auto_approve as RiskLevel | null) ?? null,
    allowedTargets:
      (row.allowed_targets as Record<string, unknown> | null) ?? null,
    deniedTargets:
      (row.denied_targets as Record<string, unknown> | null) ?? null,
    limits: (row.limits as Record<string, unknown>) ?? {},
    approverGroupId: row.approver_group_id ?? null,
    priority: row.priority,
    enabled: row.enabled,
    createdByThreadId: row.created_by_thread_id,
    createdByRunId: row.created_by_run_id,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

export async function listEnabledPolicies(
  orgId: string,
): Promise<ActionPolicyRecord[]> {
  const rows = await db()
    .select()
    .from(action_policy)
    .where(
      and(eq(action_policy.org_id, orgId), eq(action_policy.enabled, true)),
    )
    .orderBy(asc(action_policy.priority));
  return rows.map(toPolicyRecord);
}

export async function listAllPolicies(
  orgId: string,
): Promise<ActionPolicyRecord[]> {
  const rows = await db()
    .select()
    .from(action_policy)
    .where(eq(action_policy.org_id, orgId))
    .orderBy(asc(action_policy.priority), asc(action_policy.name));
  return rows.map(toPolicyRecord);
}

export type CreateActionPolicyInput = Omit<
  ActionPolicyRecord,
  "id" | "createdAt" | "updatedAt" | "createdByThreadId" | "createdByRunId" | "approverGroupId"
> & {
  approverGroupId?: string | null;
  /** Convenience for seeds and tools: "admin" means the Administrators group. */
  approverRole?: "admin" | null;
  createdByThreadId?: string | null;
  createdByRunId?: string | null;
};

export async function createActionPolicy(
  input: CreateActionPolicyInput,
): Promise<ActionPolicyRecord> {
  const approverGroupId =
    input.approverGroupId !== undefined
      ? input.approverGroupId
      : input.approverRole === "admin"
        ? await builtinGroupId(input.orgId, ADMINISTRATORS_GROUP_SLUG)
        : null;
  const [row] = await db()
    .insert(action_policy)
    .values({
      org_id: input.orgId,
      name: input.name,
      description: input.description,
      applies_to_kinds: input.appliesToKinds,
      applies_to_scopes: input.appliesToScopes,
      mode: input.mode,
      risk_threshold_auto_approve: input.riskThresholdAutoApprove,
      allowed_targets: input.allowedTargets,
      denied_targets: input.deniedTargets,
      limits: input.limits,
      approver_group_id: approverGroupId,
      priority: input.priority,
      enabled: input.enabled,
      created_by_thread_id: input.createdByThreadId ?? null,
      created_by_run_id: input.createdByRunId ?? null,
    })
    .returning();
  return toPolicyRecord(row);
}

export async function getActionPolicy(
  orgId: string,
  policyId: string,
): Promise<ActionPolicyRecord | null> {
  const rows = await db()
    .select()
    .from(action_policy)
    .where(and(eq(action_policy.org_id, orgId), eq(action_policy.id, policyId)))
    .limit(1);
  return rows[0] ? toPolicyRecord(rows[0]) : null;
}

export async function getActionPolicyByName(
  orgId: string,
  name: string,
): Promise<ActionPolicyRecord | null> {
  const rows = await db()
    .select()
    .from(action_policy)
    .where(and(eq(action_policy.org_id, orgId), eq(action_policy.name, name)))
    .limit(1);
  return rows[0] ? toPolicyRecord(rows[0]) : null;
}

export type UpsertActionPolicyResult = {
  action: "created" | "updated";
  policy: ActionPolicyRecord;
};

export async function upsertActionPolicyByName(
  input: CreateActionPolicyInput,
): Promise<UpsertActionPolicyResult> {
  const existing = await getActionPolicyByName(input.orgId, input.name);
  if (!existing) {
    const created = await createActionPolicy(input);
    return { action: "created", policy: created };
  }
  const updated = await updateActionPolicy(input.orgId, existing.id, {
    description: input.description,
    appliesToKinds: input.appliesToKinds,
    appliesToScopes: input.appliesToScopes,
    mode: input.mode,
    riskThresholdAutoApprove: input.riskThresholdAutoApprove,
    allowedTargets: input.allowedTargets,
    deniedTargets: input.deniedTargets,
    limits: input.limits,
    priority: input.priority,
    enabled: input.enabled,
  });
  if (!updated) throw new Error(`action_policy ${existing.id} disappeared`);
  // Provenance fields (createdByThreadId/createdByRunId) are creation-only:
  // an edit doesn't overwrite the originating thread, so the rule's audit
  // trail still points at the conversation that produced it the first time.
  return { action: "updated", policy: updated };
}

export type UpdateActionPolicyInput = Partial<
  Omit<CreateActionPolicyInput, "orgId">
>;

export async function updateActionPolicy(
  orgId: string,
  policyId: string,
  patch: UpdateActionPolicyInput,
): Promise<ActionPolicyRecord | null> {
  const set: Record<string, unknown> = { updated_at: new Date() };
  if (patch.name !== undefined) set.name = patch.name;
  if (patch.description !== undefined) set.description = patch.description;
  if (patch.appliesToKinds !== undefined)
    set.applies_to_kinds = patch.appliesToKinds;
  if (patch.appliesToScopes !== undefined)
    set.applies_to_scopes = patch.appliesToScopes;
  if (patch.mode !== undefined) set.mode = patch.mode;
  if (patch.riskThresholdAutoApprove !== undefined)
    set.risk_threshold_auto_approve = patch.riskThresholdAutoApprove;
  if (patch.allowedTargets !== undefined)
    set.allowed_targets = patch.allowedTargets;
  if (patch.deniedTargets !== undefined)
    set.denied_targets = patch.deniedTargets;
  if (patch.limits !== undefined) set.limits = patch.limits;
  if (patch.approverGroupId !== undefined) set.approver_group_id = patch.approverGroupId;
  if (patch.priority !== undefined) set.priority = patch.priority;
  if (patch.enabled !== undefined) set.enabled = patch.enabled;

  if (Object.keys(set).length === 1) {
    // only updated_at — nothing meaningful to write
    return getActionPolicy(orgId, policyId);
  }

  const [row] = await db()
    .update(action_policy)
    .set(set)
    .where(and(eq(action_policy.org_id, orgId), eq(action_policy.id, policyId)))
    .returning();
  return row ? toPolicyRecord(row) : null;
}

export type ActionRequestRecord = {
  id: string;
  orgId: string;
  /** SEC5: the human principal + agent backend, snapshotted at creation. */
  actorUserId: string | null;
  actorRole: string | null;
  actorBackend: string | null;
  workflowRunId: string | null;
  triggeredByObservationId: string | null;
  policyId: string | null;
  scope: ActionScope;
  kind: string;
  target: string | null;
  payload: Record<string, unknown>;
  riskLevel: RiskLevel | null;
  status: ActionRequestStatus;
  summary: string | null;
  /**
   * Agent's natural-language framing of *why* this action was
   * requested — populated for ask-mode requests so the inline
   * approval card in /work has a human-authored headline. Null
   * for auto-mode and pre-intent legacy rows.
   */
  intent: string | null;
  /** Agent-estimated, server-clamped minutes of human effort saved. */
  minutesSaved: number | null;
  minutesSavedBasis: string | null;
  /**
   * /work run id this request was emitted from. Lets the worker's
   * runActionExecute emit an action_request_result event into the
   * same run so the chat UI can render the outcome inline. Null
   * for workflow-runner-emitted requests.
   */
  workRunId: string | null;
  requestedByRunId: string | null;
  approvedByUserId: string | null;
  approvedAt: Date | null;
  rejectionReason: string | null;
  failureReason: string | null;
  createdAt: Date;
  updatedAt: Date;
};

function toRequestRecord(
  row: typeof action_request.$inferSelect,
): ActionRequestRecord {
  return {
    id: row.id,
    orgId: row.org_id,
    actorUserId: row.actor_user_id,
    actorRole: row.actor_role,
    actorBackend: row.actor_backend,
    workflowRunId: row.workflow_run_id,
    triggeredByObservationId: row.triggered_by_observation_id,
    policyId: row.policy_id,
    scope: row.scope as ActionScope,
    kind: row.kind,
    target: row.target,
    payload: (row.payload as Record<string, unknown>) ?? {},
    riskLevel: (row.risk_level as RiskLevel | null) ?? null,
    status: row.status as ActionRequestStatus,
    summary: row.summary,
    intent: row.intent ?? null,
    minutesSaved: row.minutes_saved ?? null,
    minutesSavedBasis: row.minutes_saved_basis ?? null,
    workRunId: row.work_run_id ?? null,
    requestedByRunId: row.requested_by_run_id,
    approvedByUserId: row.approved_by_user_id,
    approvedAt: row.approved_at,
    rejectionReason: row.rejection_reason,
    failureReason: row.failure_reason,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

export type CreateActionRequestInput = {
  orgId: string;
  workflowRunId?: string | null;
  triggeredByObservationId?: string | null;
  policyId?: string | null;
  scope: ActionScope;
  kind: string;
  target?: string | null;
  payload?: Record<string, unknown>;
  riskLevel?: RiskLevel | null;
  status: ActionRequestStatus;
  summary?: string | null;
  /** Agent's NL framing — required at the app layer for ask-mode rows. */
  intent?: string | null;
  /** /work run id this request was emitted from (omit for workflow paths). */
  workRunId?: string | null;
  requestedByRunId?: string | null;
  /** Agent-estimated, server-clamped minutes of human effort this saves. */
  minutesSaved?: number | null;
  minutesSavedBasis?: string | null;
  /** SEC5: dual identity. Omit to snapshot from workRunId's K1 actor + backend. */
  actorUserId?: string | null;
  actorRole?: string | null;
  actorBackend?: string | null;
};

export type ActionRequestCreatedHook = (
  request: ActionRequestRecord,
) => Promise<ActionRequestRecord | void>;

const actionRequestCreatedHooks = new Set<ActionRequestCreatedHook>();

/**
 * Register worker-owned preflight work that must finish before a newly
 * persisted action request is returned to its caller (and therefore before
 * the caller can emit an approval card or enqueue execution).
 */
export function registerActionRequestCreatedHook(
  hook: ActionRequestCreatedHook,
): () => void {
  actionRequestCreatedHooks.add(hook);
  return () => {
    actionRequestCreatedHooks.delete(hook);
  };
}

/** Replace an action payload after a successful, request-bound preflight. */
export async function updateActionRequestPayload(args: {
  id: string;
  orgId: string;
  payload: Record<string, unknown>;
}): Promise<ActionRequestRecord> {
  const [row] = await db()
    .update(action_request)
    .set({ payload: args.payload, updated_at: new Date() })
    .where(
      and(
        eq(action_request.org_id, args.orgId),
        eq(action_request.id, args.id),
      ),
    )
    .returning();
  if (!row) throw new Error(`action_request ${args.id} not found`);
  const record = toRequestRecord(row);
  const { recordAuditEvent } = await import("./audit-chain");
  await recordAuditEvent({
    orgId: args.orgId,
    entityKind: "action_request",
    entityId: record.id,
    event: "payload_prepared",
    payload: { kind: record.kind, payloadKeys: Object.keys(record.payload).sort() },
  });
  return record;
}

/**
 * For a kind with a target spec, stores the targets read from the payload and
 * re-checks an approved request against them, so no caller can approve an
 * action for targets that the rules do not allow.
 */
async function checkActionTargets(input: CreateActionRequestInput): Promise<CreateActionRequestInput> {
  const { withActionTargets } = await import("./action-targets");
  const subject = await withActionTargets(
    input.orgId,
    { scope: input.scope, kind: input.kind, target: input.target ?? null, riskLevel: input.riskLevel ?? null },
    input.payload,
  );
  if (!subject.targets) return input;
  if (input.status !== "approved") return { ...input, target: subject.target };
  const { evaluateActionPolicy } = await import("./policy-engine");
  const decision = evaluateActionPolicy(subject, await listEnabledPolicies(input.orgId));
  if (decision.decision === "deny" || decision.decision === "no_policy") {
    throw new Error(`action ${input.kind} is not allowed for its targets: ${decision.reason}`);
  }
  return {
    ...input,
    target: subject.target,
    policyId: decision.policy.id,
    status: decision.decision === "allow" ? "approved" : "pending_approval",
  };
}

export async function createActionRequest(
  rawInput: CreateActionRequestInput,
): Promise<ActionRequestRecord> {
  const input = await checkActionTargets(rawInput);
  // SEC5: snapshot the dual identity at creation. When the caller did
  // not resolve it, derive it from the originating work run (K1 actor
  // + agent backend) so every request carries who-via-which-agent.
  let actor = {
    userId: input.actorUserId ?? null,
    role: input.actorRole ?? null,
    backend: input.actorBackend ?? null,
  };
  if (input.workRunId && (!actor.role || !actor.backend)) {
    const [run] = await db()
      .select({
        userId: work_run.actor_user_id,
        role: work_run.actor_role,
        backend: work_run.backend,
      })
      .from(work_run)
      .where(eq(work_run.id, input.workRunId))
      .limit(1);
    if (run) {
      actor = {
        userId: actor.userId ?? run.userId,
        role: actor.role ?? run.role,
        backend: actor.backend ?? run.backend,
      };
    }
  }
  const [row] = await db()
    .insert(action_request)
    .values({
      org_id: input.orgId,
      actor_user_id: actor.userId,
      actor_role: actor.role,
      actor_backend: actor.backend,
      workflow_run_id: input.workflowRunId ?? null,
      triggered_by_observation_id: input.triggeredByObservationId ?? null,
      policy_id: input.policyId ?? null,
      scope: input.scope,
      kind: input.kind,
      target: input.target ?? null,
      payload: input.payload ?? {},
      risk_level: input.riskLevel ?? null,
      status: input.status,
      summary: input.summary ?? null,
      intent: input.intent ?? null,
      minutes_saved: input.minutesSaved ?? null,
      minutes_saved_basis: input.minutesSavedBasis ?? null,
      work_run_id: input.workRunId ?? null,
      requested_by_run_id: input.requestedByRunId ?? null,
    })
    .returning();
  const record = toRequestRecord(row);
  // SEC10: governance events ride the tamper-evident chain.
  const { recordAuditEvent } = await import("./audit-chain");
  await recordAuditEvent({
    orgId: input.orgId,
    entityKind: "action_request",
    entityId: record.id,
    event: `created:${record.status}`,
    payload: {
      kind: record.kind,
      scope: record.scope,
      target: record.target,
      status: record.status,
      actorUserId: record.actorUserId,
      actorRole: record.actorRole,
      actorBackend: record.actorBackend,
    },
  });
  let prepared = record;
  try {
    for (const hook of actionRequestCreatedHooks) {
      prepared = (await hook(prepared)) ?? prepared;
    }
    return prepared;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    await markActionRequestFailed(record.id, `action preflight failed: ${message}`);
    await recordAuditEvent({
      orgId: input.orgId,
      entityKind: "action_request",
      entityId: record.id,
      event: "preflight_failed",
      payload: { kind: record.kind, error: message },
    });
    throw error;
  }
}

export async function getActionRequest(
  orgId: string,
  id: string,
): Promise<ActionRequestRecord | null> {
  const rows = await db()
    .select()
    .from(action_request)
    .where(and(eq(action_request.org_id, orgId), eq(action_request.id, id)))
    .limit(1);
  return rows[0] ? toRequestRecord(rows[0]) : null;
}

export type ListActionRequestsOptions = {
  orgId: string;
  status?: ActionRequestStatus;
  workflowRunId?: string;
  limit?: number;
};

export async function listActionRequests(
  opts: ListActionRequestsOptions,
): Promise<ActionRequestRecord[]> {
  const filters = [eq(action_request.org_id, opts.orgId)];
  if (opts.status) filters.push(eq(action_request.status, opts.status));
  if (opts.workflowRunId)
    filters.push(eq(action_request.workflow_run_id, opts.workflowRunId));
  const rows = await db()
    .select()
    .from(action_request)
    .where(and(...filters))
    .orderBy(desc(action_request.created_at))
    .limit(opts.limit ?? 100);
  return rows.map(toRequestRecord);
}

export class InvalidActionStatusTransitionError extends Error {
  constructor(from: ActionRequestStatus, to: ActionRequestStatus) {
    super(`invalid action_request transition: ${from} → ${to}`);
    this.name = "InvalidActionStatusTransitionError";
  }
}

function assertTransition(
  current: ActionRequestStatus,
  next: ActionRequestStatus,
  allowedFrom: ActionRequestStatus[],
): void {
  if (!allowedFrom.includes(current)) {
    throw new InvalidActionStatusTransitionError(current, next);
  }
}

export async function approveActionRequest(args: {
  id: string;
  orgId: string;
  approverUserId: string | null;
  /** K2: when supplied, the matched policy's approver_role is enforced.
   *  Legacy callers (no actor context yet) skip the check. */
  approver?: { userId: string | null; role: "admin" | "member" | "service" };
}): Promise<ActionRequestRecord> {
  const existing = await getActionRequest(args.orgId, args.id);
  if (!existing) throw new Error(`action_request ${args.id} not found`);
  if (args.approver) await assertMayDecide(args.orgId, existing, args.approver);
  assertTransition(existing.status, "approved", ["draft", "pending_approval"]);
  const [row] = await db()
    .update(action_request)
    .set({
      status: "approved",
      approved_by_user_id: args.approverUserId,
      approved_at: new Date(),
      updated_at: new Date(),
    })
    .where(eq(action_request.id, args.id))
    .returning();
  const record = toRequestRecord(row);
  const { recordAuditEvent } = await import("./audit-chain");
  await recordAuditEvent({
    orgId: args.orgId,
    entityKind: "action_request",
    entityId: record.id,
    event: "approved",
    payload: { approvedBy: args.approverUserId, kind: record.kind,
      soloOperator: args.approver?.userId === null && args.approver.role === "admin" && resolveDeploymentProfile() === "solo",
    },
  });
  return record;
}

/** Explicit solo operator decisions are human approvals even without an SSO user row. */
export async function hasHumanActionApproval(request: ActionRequestRecord): Promise<boolean> {
  if (request.approvedByUserId) return true;
  if (!request.approvedAt) return false;
  const events = await db().select({ payload: audit_chain.payload }).from(audit_chain).where(and(
    eq(audit_chain.org_id, request.orgId), eq(audit_chain.entity_kind, "action_request"),
    eq(audit_chain.entity_id, request.id), eq(audit_chain.event, "approved"),
  ));
  return events.some(({ payload }) => (payload as Record<string, unknown>).soloOperator === true);
}

/**
 * Promote a request only after a trusted preflight has classified and capped it.
 * This is deliberately separate from human approval: no approver identity is
 * fabricated, and adapters can still require one for sensitive work.
 */
export async function autoApprovePreparedActionRequest(args: {
  id: string;
  orgId: string;
  reason: string;
}): Promise<ActionRequestRecord> {
  const existing = await getActionRequest(args.orgId, args.id);
  if (!existing) throw new Error(`action_request ${args.id} not found`);
  assertTransition(existing.status, "approved", ["draft", "pending_approval"]);
  const [row] = await db()
    .update(action_request)
    .set({
      status: "approved",
      approved_by_user_id: null,
      approved_at: new Date(),
      updated_at: new Date(),
    })
    .where(
      and(
        eq(action_request.org_id, args.orgId),
        eq(action_request.id, args.id),
      ),
    )
    .returning();
  if (!row) throw new Error(`action_request ${args.id} not found`);
  const record = toRequestRecord(row);
  const { recordAuditEvent } = await import("./audit-chain");
  await recordAuditEvent({
    orgId: args.orgId,
    entityKind: "action_request",
    entityId: record.id,
    event: "auto_approved:prepared",
    payload: { kind: record.kind, reason: args.reason },
  });
  return record;
}

/**
 * K2: enforce the matched policy's approver_role for a decision. Admin
 * always may; member may unless the policy demands a different role;
 * service principals never decide.
 */
async function assertMayDecide(
  orgId: string,
  request: ActionRequestRecord,
  approver: { userId: string | null; role: "admin" | "member" | "service" },
): Promise<void> {
  const { assertCan, ForbiddenError } = await import("../work/authz");
  const policy = request.policyId
    ? await getActionPolicy(orgId, request.policyId)
    : null;
  if (policy?.approverGroupId && approver.role !== "admin") {
    const { resolveUserGroups } = await import("@neko/db");
    const groups = approver.userId && approver.role === "member" ? await resolveUserGroups(orgId, approver.userId) : null;
    if (!groups?.groupIds.includes(policy.approverGroupId)) {
      throw new ForbiddenError(`action_request ${request.id}: approval needs a member of the policy's approver group`);
    }
    return;
  }
  assertCan(
    { userId: approver.userId, role: approver.role },
    "approve",
    { kind: "action_approval", approverRole: null },
    `action_request ${request.id}`,
  );
}

export async function rejectActionRequest(args: {
  id: string;
  orgId: string;
  approverUserId: string | null;
  reason?: string;
  /** K2: same gate as approve — rejecting is a decision too. */
  approver?: { userId: string | null; role: "admin" | "member" | "service" };
}): Promise<ActionRequestRecord> {
  const existing = await getActionRequest(args.orgId, args.id);
  if (!existing) throw new Error(`action_request ${args.id} not found`);
  if (args.approver) await assertMayDecide(args.orgId, existing, args.approver);
  assertTransition(existing.status, "rejected", ["draft", "pending_approval"]);
  const [row] = await db()
    .update(action_request)
    .set({
      status: "rejected",
      approved_by_user_id: args.approverUserId,
      approved_at: new Date(),
      rejection_reason: args.reason ?? null,
      updated_at: new Date(),
    })
    .where(eq(action_request.id, args.id))
    .returning();
  const record = toRequestRecord(row);
  const { recordAuditEvent } = await import("./audit-chain");
  await recordAuditEvent({
    orgId: args.orgId,
    entityKind: "action_request",
    entityId: record.id,
    event: "rejected",
    payload: {
      rejectedBy: args.approverUserId,
      reason: args.reason ?? null,
      kind: record.kind,
    },
  });
  return record;
}

export async function markActionRequestExecuted(
  id: string,
): Promise<void> {
  await db()
    .update(action_request)
    .set({ status: "executed", updated_at: new Date() })
    .where(eq(action_request.id, id));
}

export async function markActionRequestFailed(
  id: string,
  error: string,
): Promise<void> {
  await db()
    .update(action_request)
    .set({
      status: "failed",
      failure_reason: error,
      rejection_reason: null,
      updated_at: new Date(),
    })
    .where(eq(action_request.id, id));
}

export class UnsafeActionRetryError extends Error {
  constructor() {
    super("This action cannot be retried safely. Check its change set before proposing another action.");
    this.name = "UnsafeActionRetryError";
  }
}

/** Only a single failed Magento change set with entirely unattempted rows is retryable. */
export async function canRetryActionRequest(orgId: string, id: string): Promise<boolean> {
  const result = await pool().query<{ eligible: boolean }>(`
    SELECT EXISTS (
      SELECT 1 FROM action_request a
      JOIN action_changeset c ON c.action_request_id = a.id
      WHERE a.org_id = $1 AND a.id = $2 AND a.status = 'failed'
        AND c.org_id = a.org_id AND c.status = 'failed' AND c.bulk_uuid IS NULL
        AND (SELECT count(*) FROM action_changeset WHERE action_request_id = a.id) = 1
        AND EXISTS (SELECT 1 FROM action_changeset_row WHERE changeset_id = c.id)
        AND NOT EXISTS (
          SELECT 1 FROM action_changeset_row r WHERE r.changeset_id = c.id
            AND (r.status <> 'failed' OR r.started_at IS NOT NULL OR r.external_ref IS NOT NULL)
        )
    ) AS eligible`, [orgId, id]);
  return result.rows[0]?.eligible ?? false;
}

export async function retryActionRequest(args: {
  id: string;
  orgId: string;
  actor: { userId: string | null; role: "admin" | "member" | "service" };
}): Promise<ActionRequestRecord> {
  const client = await pool().connect();
  try {
    await client.query("BEGIN");
    const request = await client.query<{ status: ActionRequestStatus }>(
      "SELECT status FROM action_request WHERE org_id = $1 AND id = $2 FOR UPDATE",
      [args.orgId, args.id],
    );
    if (!request.rows[0]) throw new Error(`action_request ${args.id} not found`);
    assertTransition(request.rows[0].status, "approved", ["failed"]);
    const existing = await getActionRequest(args.orgId, args.id);
    if (!existing) throw new Error(`action_request ${args.id} not found`);
    await assertMayDecide(args.orgId, existing, args.actor);

    const changesets = await client.query<{ id: string; status: string; bulk_uuid: string | null }>(
      "SELECT id, status, bulk_uuid FROM action_changeset WHERE org_id = $1 AND action_request_id = $2 FOR UPDATE",
      [args.orgId, args.id],
    );
    if (changesets.rows.length !== 1 || changesets.rows[0].status !== "failed" || changesets.rows[0].bulk_uuid !== null) {
      throw new UnsafeActionRetryError();
    }
    const changesetId = changesets.rows[0].id;
    const rows = await client.query<{ status: string; started_at: Date | null; external_ref: string | null }>(
      "SELECT status, started_at, external_ref FROM action_changeset_row WHERE changeset_id = $1 FOR UPDATE",
      [changesetId],
    );
    if (rows.rows.length === 0 || rows.rows.some((row) => row.status !== "failed" || row.started_at !== null || row.external_ref !== null)) {
      throw new UnsafeActionRetryError();
    }
    await client.query(
      "UPDATE action_changeset_row SET status = 'draft', error = NULL, finished_at = NULL, updated_at = now() WHERE changeset_id = $1",
      [changesetId],
    );
    await client.query(
      "UPDATE action_changeset SET status = 'approved', executed_at = NULL, reconciled_at = NULL, updated_at = now() WHERE id = $1",
      [changesetId],
    );
    await client.query(
      "UPDATE action_request SET status = 'approved', failure_reason = NULL, updated_at = now() WHERE org_id = $1 AND id = $2",
      [args.orgId, args.id],
    );
    await client.query("COMMIT");
  } catch (error) {
    await client.query("ROLLBACK");
    throw error;
  } finally {
    client.release();
  }
  const record = await getActionRequest(args.orgId, args.id);
  if (!record) throw new Error(`action_request ${args.id} not found after retry`);
  const { recordAuditEvent } = await import("./audit-chain");
  await recordAuditEvent({
    orgId: args.orgId,
    entityKind: "action_request",
    entityId: args.id,
    event: "retried",
    payload: { retriedBy: args.actor.userId, kind: record.kind },
  });
  return record;
}

export type ActionExecutionRecord = {
  id: string;
  orgId: string;
  actionRequestId: string;
  changesetId: string | null;
  executor: string;
  commandOrOperation: string | null;
  payload: Record<string, unknown> | null;
  result: Record<string, unknown> | null;
  externalRef: string | null;
  status: ActionExecutionStatus;
  error: string | null;
  startedAt: Date | null;
  finishedAt: Date | null;
  createdAt: Date;
};

function toExecutionRecord(
  row: typeof action_execution.$inferSelect,
): ActionExecutionRecord {
  return {
    id: row.id,
    orgId: row.org_id,
    actionRequestId: row.action_request_id,
    changesetId: row.changeset_id,
    executor: row.executor,
    commandOrOperation: row.command_or_operation,
    payload: (row.payload as Record<string, unknown> | null) ?? null,
    result: (row.result as Record<string, unknown> | null) ?? null,
    externalRef: row.external_ref,
    status: row.status as ActionExecutionStatus,
    error: row.error,
    startedAt: row.started_at,
    finishedAt: row.finished_at,
    createdAt: row.created_at,
  };
}

export async function recordActionExecution(args: {
  orgId: string;
  actionRequestId: string;
  executor: string;
  changesetId?: string | null;
  commandOrOperation?: string | null;
  payload?: Record<string, unknown> | null;
}): Promise<ActionExecutionRecord> {
  const [row] = await db()
    .insert(action_execution)
    .values({
      org_id: args.orgId,
      action_request_id: args.actionRequestId,
      changeset_id: args.changesetId ?? null,
      executor: args.executor,
      command_or_operation: args.commandOrOperation ?? null,
      payload: args.payload ?? null,
      status: "running",
      started_at: new Date(),
    })
    .returning();
  return toExecutionRecord(row);
}

export async function finishActionExecution(args: {
  id: string;
  status: "succeeded" | "failed" | "reconcile_required" | "partially_applied";
  result?: Record<string, unknown> | null;
  externalRef?: string | null;
  changesetId?: string | null;
  commandOrOperation?: string | null;
  error?: string | null;
}): Promise<ActionExecutionRecord> {
  const [row] = await db()
    .update(action_execution)
    .set({
      status: args.status,
      result: args.result ?? null,
      external_ref: args.externalRef ?? null,
      ...(args.changesetId !== undefined ? { changeset_id: args.changesetId } : {}),
      ...(args.commandOrOperation !== undefined
        ? { command_or_operation: args.commandOrOperation }
        : {}),
      error: args.error ?? null,
      finished_at: new Date(),
    })
    .where(eq(action_execution.id, args.id))
    .returning();
  const record = toExecutionRecord(row);
  const { recordAuditEvent } = await import("./audit-chain");
  await recordAuditEvent({
    orgId: record.orgId,
    entityKind: "action_execution",
    entityId: record.id,
    event: `execution:${record.status}`,
    payload: {
      actionRequestId: record.actionRequestId,
      executor: record.executor,
      status: record.status,
      externalRef: record.externalRef,
      error: record.error,
    },
  });
  return record;
}

export async function listActionExecutions(
  actionRequestId: string,
): Promise<ActionExecutionRecord[]> {
  const rows = await db()
    .select()
    .from(action_execution)
    .where(eq(action_execution.action_request_id, actionRequestId))
    .orderBy(desc(action_execution.created_at));
  return rows.map(toExecutionRecord);
}
