import "server-only";

import {
  buildRecordDetailQuery,
  evaluateRecordObjectPermission,
  RecordReadPermissionError,
  RecordValidationError,
  validateRecordFields,
  type RecordField,
  type RecordValidationIssue,
  type RecordViewColumn,
} from "@neko/records";
import { resolveUserGroups } from "@neko/db";
import { can, inProcessControlPlane, type RunActor } from "@neko/llm/work";
import { approveActionRequest } from "@neko/llm/workflows";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import {
  loadRecordAppShell,
  readRecordDetail,
  readRecordRecycleDetail,
  RecordAppRouteError,
  type RecordAppShell,
} from "@/lib/records";

export type RecordFormOperation = "create" | "update";

export type RecordFormModel = {
  appId: string;
  appLabel: string;
  objectApiName: string;
  objectLabel: string;
  objectPluralLabel: string;
  nameField: string;
  operation: RecordFormOperation;
  recordId: string | null;
  fields: RecordViewColumn[];
  row: Record<string, unknown> | null;
};

export type RecordFormSubmitResult = {
  status: "executed" | "queued";
  actionRequestId: string;
  recordId: string | null;
  policy: string;
};

export class RecordFormPolicyError extends Error {
  readonly code = "records_form_policy_denied";

  constructor(message: string) {
    super(message);
    this.name = "RecordFormPolicyError";
  }
}

export class RecordFormExecutionError extends Error {
  readonly code = "records_form_execution_failed";

  constructor(
    message: string,
    readonly actionRequestId: string,
  ) {
    super(message);
    this.name = "RecordFormExecutionError";
  }
}

function roleForActor(role: string): "admin" | "member" {
  return role === "member" ? "member" : "admin";
}

function editableColumns(columns: RecordViewColumn[]): RecordViewColumn[] {
  return columns.filter(
    (column) =>
      column.kind !== "id" &&
      column.kind !== "owner" &&
      column.kind !== "system_datetime",
  );
}

function viewAllows(
  view: ReturnType<typeof resolveFormView>,
  role: "admin" | "member",
  operation: "create" | "update" | "delete",
): boolean {
  return evaluateRecordObjectPermission({
    actor: { role },
    object: {
      appId: view.object.appId,
      apiName: view.object.apiName,
      visibility: view.object.visibility,
      archived: view.object.archivedAt !== null,
    },
    grant: view.permission,
    operation,
  });
}

function resolveFormView(
  shell: RecordAppShell,
  objectApiName: string,
  role: "admin" | "member",
) {
  return buildRecordDetailQuery({
    snapshot: shell.snapshot,
    objectApiName,
    role,
    recordId: "records-form-model",
    allFields: true,
  }).view;
}

/** The policy's approver group decides; administrators always may. */
async function mayApprove(actor: RunActor, approverGroupId: string | null): Promise<boolean> {
  if (!can(actor, "approve", { kind: "action_approval", approverRole: null })) return false;
  if (!approverGroupId || actor.role === "admin") return true;
  if (!actor.userId) return false;
  const groups = await resolveUserGroups(await getOrgId(), actor.userId);
  return groups.groupIds.includes(approverGroupId);
}

export async function getRecordFormModel(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
  operation: RecordFormOperation;
  recordId?: string;
}): Promise<RecordFormModel> {
  const [shell, actor] = await Promise.all([
    loadRecordAppShell(input.orgId, input.appId),
    getCurrentActor(),
  ]);
  if (shell.availability !== "active") {
    throw new RecordAppRouteError(
      shell.degradedReason ?? "The records data plane is degraded.",
      503,
    );
  }
  const role = roleForActor(actor.role);
  const view = resolveFormView(shell, input.objectApiName, role);
  const allowed = viewAllows(view, role, input.operation);
  if (!allowed) throw new RecordReadPermissionError();

  let row: Record<string, unknown> | null = null;
  if (input.operation === "update") {
    if (!input.recordId?.trim()) throw new RecordReadPermissionError();
    const detail = await readRecordDetail({
      orgId: input.orgId,
      appId: shell.snapshot.app.appId,
      objectApiName: view.object.apiName,
      recordId: input.recordId,
      allFields: true,
    });
    row = detail.row;
  }

  return {
    appId: shell.snapshot.app.appId,
    appLabel: shell.snapshot.app.label,
    objectApiName: view.object.apiName,
    objectLabel: view.object.label,
    objectPluralLabel: view.object.pluralLabel,
    nameField: view.object.nameField,
    operation: input.operation,
    recordId: input.operation === "update" ? input.recordId ?? null : null,
    fields: editableColumns(view.columns),
    row,
  };
}

function registryFields(shell: RecordAppShell, objectId: string): RecordField[] {
  return shell.snapshot.fields.filter(
    (field) => field.objectId === objectId && field.archivedAt === null,
  );
}

function validationObject(value: unknown, label: string): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    const issues: RecordValidationIssue[] = [
      { field: label, code: "type", message: "must be an object" },
    ];
    throw new RecordValidationError(issues);
  }
  return { ...(value as Record<string, unknown>) };
}

export async function submitRecordFormAction(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
  operation: RecordFormOperation;
  recordId?: string;
  fields: unknown;
  expected?: unknown;
}): Promise<RecordFormSubmitResult> {
  const [shell, actor] = await Promise.all([
    loadRecordAppShell(input.orgId, input.appId),
    getCurrentActor(),
  ]);
  if (shell.availability !== "active") {
    throw new RecordAppRouteError(
      shell.degradedReason ?? "The records data plane is degraded.",
      503,
    );
  }
  const role = roleForActor(actor.role);
  const view = resolveFormView(shell, input.objectApiName, role);
  const permission = viewAllows(view, role, input.operation);
  if (!permission) throw new RecordReadPermissionError();

  const fields = validationObject(input.fields, "fields");
  const objectFields = registryFields(shell, view.object.id);
  const validatedFields = validateRecordFields({
    values: fields,
    registryFields: objectFields,
    operation: input.operation,
  });

  let expected: Record<string, unknown> | undefined;
  let recordId: string | undefined;
  if (input.operation === "update") {
    recordId = input.recordId?.trim();
    if (!recordId) {
      throw new RecordValidationError([
        { field: "id", code: "required", message: "is required for updates" },
      ]);
    }
    expected = validationObject(input.expected, "expected");
    expected = validateRecordFields({
      values: expected,
      registryFields: objectFields,
      operation: "expected",
    }).fields;
  }

  const kind = input.operation === "create" ? "record_create" : "record_update";
  const target = `record:${shell.snapshot.app.appId}/${view.object.apiName}`;
  const decision = await inProcessControlPlane.evaluateActionPolicy({
    orgId: input.orgId,
    scope: "internal",
    kind,
    target,
    riskLevel: "low",
  });
  if (decision.decision === "no_policy") {
    throw new RecordFormPolicyError(
      "No action policy covers this record change. Define one in Rules before submitting.",
    );
  }
  if (decision.decision === "deny") {
    throw new RecordFormPolicyError(decision.reason);
  }
  if (!(await mayApprove(actor, decision.policy.approverGroupId))) {
    throw new RecordFormPolicyError("This change needs approval from the policy's approver group.");
  }

  const payload: Record<string, unknown> = {
    app: shell.snapshot.app.appId,
    object: view.object.apiName,
    fields: validatedFields.fields,
  };
  if (input.operation === "update") {
    payload.id = recordId;
    payload.expected = expected;
  }
  const summary = `${input.operation === "create" ? "Create" : "Update"} ${view.object.label} from generated app form`;
  const request = await inProcessControlPlane.createActionRequest({
    orgId: input.orgId,
    scope: "internal",
    kind,
    target,
    payload,
    riskLevel: "low",
    status: decision.mode === "draft_only" ? "draft" : "pending_approval",
    policyId: decision.policy.id,
    summary,
    intent: `${summary}. The signed-in operator submitted this form directly.`,
    actorUserId: actor.userId,
    actorRole: role,
    actorBackend: "web-form",
  });
  await approveActionRequest({
    id: request.id,
    orgId: input.orgId,
    approverUserId: actor.userId,
    approver: { userId: actor.userId, role },
  });
  await inProcessControlPlane.enqueueActionExecute({
    orgId: input.orgId,
    actionRequestId: request.id,
  });
  const execution = await inProcessControlPlane.waitForActionExecution({
    orgId: input.orgId,
    actionRequestId: request.id,
    timeoutMs: 45_000,
  });
  if (execution.status === "failed") {
    throw new RecordFormExecutionError(execution.error, request.id);
  }
  if (execution.status === "timeout") {
    return {
      status: "queued",
      actionRequestId: request.id,
      recordId: recordId ?? null,
      policy: decision.policy.name,
    };
  }
  const result = execution.outcome.result;
  return {
    status: "executed",
    actionRequestId: request.id,
    recordId:
      result && typeof result.id === "string" ? result.id : recordId ?? null,
    policy: decision.policy.name,
  };
}

export async function submitRecordRestoreAction(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
  recordId: string;
}): Promise<RecordFormSubmitResult> {
  const [recycled, actor] = await Promise.all([
    readRecordRecycleDetail(input),
    getCurrentActor(),
  ]);
  if (!recycled.row) throw new RecordReadPermissionError();
  const role = roleForActor(actor.role);
  if (
    !viewAllows(recycled.view, role, "update")
  ) {
    throw new RecordReadPermissionError();
  }
  const target = `record:${recycled.app.appId}/${recycled.view.object.apiName}`;
  const decision = await inProcessControlPlane.evaluateActionPolicy({
    orgId: input.orgId,
    scope: "internal",
    kind: "record_restore",
    target,
    riskLevel: "low",
  });
  if (decision.decision === "no_policy") {
    throw new RecordFormPolicyError(
      "No action policy covers this restore. Define one in Rules before submitting.",
    );
  }
  if (decision.decision === "deny") {
    throw new RecordFormPolicyError(decision.reason);
  }
  if (!(await mayApprove(actor, decision.policy.approverGroupId))) {
    throw new RecordFormPolicyError("This restore needs approval from the policy's approver group.");
  }

  const summary = `Restore ${recycled.view.object.label} ${recycled.row.recordId}`;
  const request = await inProcessControlPlane.createActionRequest({
    orgId: input.orgId,
    scope: "internal",
    kind: "record_restore",
    target,
    payload: {
      app: recycled.app.appId,
      object: recycled.view.object.apiName,
      id: recycled.row.recordId,
    },
    riskLevel: "low",
    status: decision.mode === "draft_only" ? "draft" : "pending_approval",
    policyId: decision.policy.id,
    summary,
    intent: `${summary}. The signed-in operator submitted this restore from the recycle bin.`,
    actorUserId: actor.userId,
    actorRole: role,
    actorBackend: "web-form",
  });
  await approveActionRequest({
    id: request.id,
    orgId: input.orgId,
    approverUserId: actor.userId,
    approver: { userId: actor.userId, role },
  });
  await inProcessControlPlane.enqueueActionExecute({
    orgId: input.orgId,
    actionRequestId: request.id,
  });
  const execution = await inProcessControlPlane.waitForActionExecution({
    orgId: input.orgId,
    actionRequestId: request.id,
    timeoutMs: 45_000,
  });
  if (execution.status === "failed") {
    throw new RecordFormExecutionError(execution.error, request.id);
  }
  if (execution.status === "timeout") {
    return {
      status: "queued",
      actionRequestId: request.id,
      recordId: recycled.row.recordId,
      policy: decision.policy.name,
    };
  }
  return {
    status: "executed",
    actionRequestId: request.id,
    recordId: recycled.row.recordId,
    policy: decision.policy.name,
  };
}

export async function submitRecordDeleteAction(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
  recordId: string;
}): Promise<RecordFormSubmitResult> {
  const [shell, actor] = await Promise.all([
    loadRecordAppShell(input.orgId, input.appId),
    getCurrentActor(),
  ]);
  if (shell.availability !== "active") {
    throw new RecordAppRouteError(
      shell.degradedReason ?? "The records data plane is degraded.",
      503,
    );
  }
  const role = roleForActor(actor.role);
  const view = resolveFormView(shell, input.objectApiName, role);
  if (!viewAllows(view, role, "delete")) throw new RecordReadPermissionError();
  const recordId = input.recordId.trim();
  const detail = await readRecordDetail({
    orgId: input.orgId,
    appId: shell.snapshot.app.appId,
    objectApiName: view.object.apiName,
    recordId,
  });
  if (!detail.row) throw new RecordReadPermissionError();

  const target = `record:${shell.snapshot.app.appId}/${view.object.apiName}`;
  const decision = await inProcessControlPlane.evaluateActionPolicy({
    orgId: input.orgId,
    scope: "internal",
    kind: "record_delete",
    target,
    riskLevel: "low",
  });
  if (decision.decision === "no_policy") {
    throw new RecordFormPolicyError(
      "No action policy covers deleting this record. Define one in Rules before you delete it.",
    );
  }
  if (decision.decision === "deny") {
    throw new RecordFormPolicyError(decision.reason);
  }
  if (!(await mayApprove(actor, decision.policy.approverGroupId))) {
    throw new RecordFormPolicyError("This delete needs approval from the policy's approver group.");
  }

  const name = String(detail.row[view.object.nameField] ?? recordId);
  const summary = `Delete ${view.object.label} ${name}`;
  const request = await inProcessControlPlane.createActionRequest({
    orgId: input.orgId,
    scope: "internal",
    kind: "record_delete",
    target,
    payload: { app: shell.snapshot.app.appId, object: view.object.apiName, id: recordId },
    riskLevel: "low",
    status: decision.mode === "draft_only" ? "draft" : "pending_approval",
    policyId: decision.policy.id,
    summary,
    intent: `${summary}. The signed-in operator deleted it from the record page; it moves to the recycle bin.`,
    actorUserId: actor.userId,
    actorRole: role,
    actorBackend: "web-form",
  });
  await approveActionRequest({
    id: request.id,
    orgId: input.orgId,
    approverUserId: actor.userId,
    approver: { userId: actor.userId, role },
  });
  await inProcessControlPlane.enqueueActionExecute({
    orgId: input.orgId,
    actionRequestId: request.id,
  });
  const execution = await inProcessControlPlane.waitForActionExecution({
    orgId: input.orgId,
    actionRequestId: request.id,
    timeoutMs: 45_000,
  });
  if (execution.status === "failed") {
    throw new RecordFormExecutionError(execution.error, request.id);
  }
  return {
    status: execution.status === "timeout" ? "queued" : "executed",
    actionRequestId: request.id,
    recordId,
    policy: decision.policy.name,
  };
}
