import { createHash } from "node:crypto";
import type { Pool } from "pg";
import {
  claimRecordsActionExecution,
  failRecordsActionExecution,
  setRecordsActionExecutionContext,
  succeedRecordsActionExecution,
} from "../actions/execution";
import type { RecordsGraphjinTransport } from "../graphjin/client";
import { validateRecordIdentifier } from "../naming";
import {
  evaluateRecordPermission,
  selectRecordPolicyGrant,
  type RecordCrudOperation,
  type RecordPolicyActor,
  type RecordPolicyGrant,
} from "../policy/evaluate";
import { RECORD_DATA_COLUMN, RECORD_SYSTEM_COLUMNS } from "../policy/graphjin";
import { authorizeRecordSnapshot } from "../policy/access";
import { RecordRegistry } from "../registry";
import type {
  AppRegistrySnapshot,
  RecordField,
  RecordObject,
} from "../types";
import {
  RecordValidationError,
  validateRecordFields,
  type RecordReferenceCheck,
} from "./validate";

export type RecordWriteOperation = "create" | "update" | "delete" | "restore";
export type RecordWriteRequestOperation = RecordWriteOperation | "upsert";

export type RecordAppWriteMode = "mirror" | "cutting_over" | "primary";

export type RecordWriteRequest = {
  actionRequestId: string;
  orgId: string;
  appId: string;
  objectApiName: string;
  operation: RecordWriteRequestOperation;
  actor: RecordPolicyActor;
  /** Worker-only connector context. Action adapters never accept this field. */
  system?: { kind: "sync"; sourceInstanceId: string };
  id?: string;
  fields?: Record<string, unknown>;
  expected?: Record<string, unknown>;
};

export type RecordWriteResult = {
  actionRequestId: string;
  appId: string;
  objectApiName: string;
  tableName: string;
  id: string;
  operation: RecordWriteOperation;
  mutationId: string;
  replayed: boolean;
  recovered: boolean;
  noOp?: boolean;
};

export class RecordWriteTargetError extends Error {
  readonly code = "records_write_target_invalid";

  constructor(message: string) {
    super(message);
    this.name = "RecordWriteTargetError";
  }
}

export class RecordPermissionDeniedError extends Error {
  readonly code = "records_permission_denied";

  constructor() {
    super("record operation is not permitted");
    this.name = "RecordPermissionDeniedError";
  }
}

export class RecordReferenceMissingError extends Error {
  readonly code = "records_reference_missing";

  constructor(readonly fieldApiName: string) {
    super(`${fieldApiName}: referenced record does not exist`);
    this.name = "RecordReferenceMissingError";
  }
}

export class RecordConcurrencyConflictError extends Error {
  readonly code = "records_concurrency_conflict";

  constructor() {
    super("record changed after it was read; refresh and retry with new expected values");
    this.name = "RecordConcurrencyConflictError";
  }
}

export class RecordNotFoundOrDeniedError extends Error {
  readonly code = "records_not_found_or_denied";

  constructor() {
    super("record was not found or is outside the actor scope");
    this.name = "RecordNotFoundOrDeniedError";
  }
}

export class RecordMutationAuditMissingError extends Error {
  readonly code = "records_mutation_audit_missing";

  constructor() {
    super("record mutation returned without its durable audit entry");
    this.name = "RecordMutationAuditMissingError";
  }
}

export class RecordMirrorWriteBlockedError extends Error {
  readonly code = "records_mirror_write_blocked";

  constructor(readonly mode: Exclude<RecordAppWriteMode, "primary">) {
    super(
      mode === "mirror"
        ? "mirrored from Salesforce — writes happen there until cutover"
        : "Salesforce cutover is in progress — local writes will open after the final sync is verified",
    );
    this.name = "RecordMirrorWriteBlockedError";
  }
}

export class RecordSyncWriteBlockedError extends Error {
  readonly code = "records_sync_write_blocked";

  constructor(readonly mode: RecordAppWriteMode | null) {
    super(
      mode === "primary"
        ? "Salesforce sync is disabled after primary cutover"
        : "Salesforce sync requires a connector-managed mirror or cutover app",
    );
    this.name = "RecordSyncWriteBlockedError";
  }
}

function deterministicMutationId(
  request: RecordWriteRequest,
  operation: RecordWriteRequestOperation,
  id: string,
): string {
  return createHash("sha256")
    .update(
      `${request.actionRequestId}\u0000${operation}\u0000${request.appId}\u0000${request.objectApiName}\u0000${id}`,
      "utf8",
    )
    .digest("hex");
}

function deterministicRecordId(actionRequestId: string): string {
  const hex = createHash("sha256").update(actionRequestId, "utf8").digest("hex").slice(0, 32);
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-4${hex.slice(13, 16)}-a${hex.slice(
    17,
    20,
  )}-${hex.slice(20)}`;
}

function objectFields(snapshot: AppRegistrySnapshot, object: RecordObject): RecordField[] {
  return snapshot.fields.filter((field) => field.objectId === object.id);
}

function operationGrant(operation: RecordWriteRequestOperation): RecordCrudOperation {
  if (operation === "upsert") return "create";
  if (operation === "restore") return "update";
  return operation;
}

function terminalError(error: unknown): error is Error & { code: string } {
  return (
    error instanceof RecordValidationError ||
    error instanceof RecordPermissionDeniedError ||
    error instanceof RecordReferenceMissingError ||
    error instanceof RecordConcurrencyConflictError ||
    error instanceof RecordNotFoundOrDeniedError ||
    error instanceof RecordMirrorWriteBlockedError ||
    error instanceof RecordSyncWriteBlockedError
  );
}

function durableError(error: Error & { code: string }): Record<string, unknown> {
  return { code: error.code, message: error.message, at: new Date().toISOString() };
}

function resultRows(data: Record<string, unknown>, tableName: string): Array<Record<string, unknown>> {
  const value = data[tableName];
  if (!Array.isArray(value)) return [];
  return value.filter(
    (row): row is Record<string, unknown> =>
      typeof row === "object" && row !== null && !Array.isArray(row),
  );
}

function buildMutationWhere(input: {
  id: string;
  restore: boolean;
  includeDeleted?: boolean;
  memberOwnerUserId: string | null;
  priorMutationId: string | null;
}): { document: string; variables: Record<string, unknown> } {
  const clauses = ["id: { eq: $record_id }"];
  if (!input.includeDeleted) {
    clauses.push(
      `${RECORD_SYSTEM_COLUMNS.deletedAt}: { is_null: ${input.restore ? "false" : "true"} }`,
    );
  }
  const variables: Record<string, unknown> = { record_id: input.id };
  if (input.memberOwnerUserId !== null) {
    clauses.push(`${RECORD_SYSTEM_COLUMNS.ownerUserId}: { eq: $actor_user_id }`);
    variables.actor_user_id = input.memberOwnerUserId;
  }
  if (input.priorMutationId !== null) {
    clauses.push(`${RECORD_SYSTEM_COLUMNS.mutationId}: { eq: $prior_mutation_id }`);
    variables.prior_mutation_id = input.priorMutationId;
  }
  return { document: `{ ${clauses.join(", ")} }`, variables };
}

function sameValue(left: unknown, right: unknown): boolean {
  if (left === right) return true;
  if (left === null || right === null || typeof left !== "object" || typeof right !== "object") {
    return false;
  }
  if (Array.isArray(left) !== Array.isArray(right)) return false;
  const leftKeys = Object.keys(left as object);
  const rightKeys = Object.keys(right as object);
  return (
    leftKeys.length === rightKeys.length &&
    leftKeys.every((key) =>
      sameValue((left as Record<string, unknown>)[key], (right as Record<string, unknown>)[key]),
    )
  );
}

function recordDocument(row: Record<string, unknown> | undefined): Record<string, unknown> {
  const value = row?.[RECORD_DATA_COLUMN];
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

export class RecordWriteExecutor {
  private readonly registry: RecordRegistry;

  constructor(
    private readonly dependencies: {
      pool: Pool;
      graphjin: RecordsGraphjinTransport;
      serviceToken: (orgId: string) => Promise<string> | string;
      leaseOwner: string;
      recordSourceWrite: (input: {
        actionRequestId: string;
        orgId: string;
        tableName: string;
        recordId: string;
      }) => Promise<void>;
      /** Metadata-plane connector mode. Omitted for unconnected/local apps. */
      appWriteMode?: (
        orgId: string,
        appId: string,
      ) => Promise<RecordAppWriteMode | null>;
      /** Fail closed when the records filesystem has reached hard-stop. */
      assertWritesAllowed?: () => Promise<void>;
      now?: () => Date;
      registry?: RecordRegistry;
    },
  ) {
    this.registry = dependencies.registry ?? new RecordRegistry(dependencies.pool);
  }

  private async resolve(request: RecordWriteRequest): Promise<{
    snapshot: AppRegistrySnapshot;
    object: RecordObject;
    fields: RecordField[];
    grant: RecordPolicyGrant | null;
  }> {
    const registrySnapshot = await this.registry.loadApp(request.orgId, request.appId);
    if (!registrySnapshot || registrySnapshot.app.status !== "active") {
      throw new RecordWriteTargetError("record app is missing or not active");
    }
    const snapshot = request.system?.kind === "sync"
      ? registrySnapshot
      : await authorizeRecordSnapshot(
          this.dependencies.pool,
          registrySnapshot,
          request.actor,
        );
    if (!snapshot) throw new RecordPermissionDeniedError();
    const objectApiName = validateRecordIdentifier(request.objectApiName);
    const object = snapshot.objects.find(
      (candidate) => candidate.apiName === objectApiName && candidate.archivedAt === null,
    );
    if (!object) throw new RecordWriteTargetError("record object is unknown or archived");
    const grant = selectRecordPolicyGrant(snapshot.permissions, {
      appId: request.appId,
      role: request.actor.role,
      objectApiName: object.apiName,
    });
    return { snapshot, object, fields: objectFields(snapshot, object), grant };
  }

  private assertPermission(
    request: RecordWriteRequest,
    object: RecordObject,
    grant: RecordPolicyGrant | null,
    ownerUserId: string | null,
  ): void {
    if (
      !evaluateRecordPermission({
        actor: request.actor,
        object: {
          appId: object.appId,
          apiName: object.apiName,
          visibility: object.visibility,
          archived: object.archivedAt !== null,
        },
        grant,
        operation: operationGrant(request.operation),
        row:
          object.visibility === "owner"
            ? { ownerUserId }
            : undefined,
      })
    ) {
      throw new RecordPermissionDeniedError();
    }
  }

  private async referenceExists(
    snapshot: AppRegistrySnapshot,
    reference: RecordReferenceCheck,
    token: string,
  ): Promise<boolean> {
    for (const rawTarget of reference.targetObjectApiNames) {
      const targetApiName = validateRecordIdentifier(rawTarget);
      const target = snapshot.objects.find(
        (object) => object.apiName === targetApiName && object.archivedAt === null,
      );
      if (!target) continue;
      const operationName = "RecordsReferenceCheck";
      const data = await this.dependencies.graphjin.execute<Record<string, unknown>>({
        operationName,
        query: `query ${operationName} { ${target.tableName}(where: { id: { eq: $id } }, limit: 1) { id } }`,
        variables: { id: reference.value },
        token,
      });
      if (resultRows(data, target.tableName).length > 0) return true;
    }
    return false;
  }

  /**
   * Compare the caller's expected values with the stored document. The
   * update then applies only while the row still carries the mutation id
   * read here, so the check and the write cannot interleave with another.
   */
  private async checkExpected(
    object: RecordObject,
    id: string,
    expected: Record<string, unknown>,
    token: string,
  ): Promise<string> {
    const operationName = "RecordsExpectedState";
    const data = await this.dependencies.graphjin.execute<Record<string, unknown>>({
      operationName,
      query: `query ${operationName} { ${object.tableName}(where: { id: { eq: $id }, ${RECORD_SYSTEM_COLUMNS.deletedAt}: { is_null: true } }, limit: 1) { id ${RECORD_DATA_COLUMN} ${RECORD_SYSTEM_COLUMNS.mutationId} } }`,
      variables: { id },
      token,
    });
    const row = resultRows(data, object.tableName)[0];
    if (!row) throw new RecordNotFoundOrDeniedError();
    const document = recordDocument(row);
    for (const [key, value] of Object.entries(expected)) {
      if (!sameValue(document[key] ?? null, value ?? null)) {
        throw new RecordConcurrencyConflictError();
      }
    }
    return String(row[RECORD_SYSTEM_COLUMNS.mutationId] ?? "");
  }

  private async auditExists(input: {
    mutationId: string;
    actionRequestId: string;
    orgId: string;
    appId: string;
    objectApiName: string;
    recordId: string;
  }): Promise<boolean> {
    const result = await this.dependencies.pool.query(
      `select 1 from engine.record_change_log
       where mutation_id = $1 and action_request_id = $2 and org_id = $3
         and app_id = $4 and object_api_name = $5 and record_id = $6`,
      [
        input.mutationId,
        input.actionRequestId,
        input.orgId,
        input.appId,
        input.objectApiName,
        input.recordId,
      ],
    );
    return result.rowCount === 1;
  }

  /**
   * Connector writes still use the generic app trigger. Normalize its durable
   * engine metadata before acknowledging the receipt so source deletes do not
   * masquerade as human recycle-bin actions. A retry completes this transaction
   * before the target cursor is allowed to advance.
   */
  private async normalizeSyncAudit(input: {
    mutationId: string;
    actionRequestId: string;
    orgId: string;
    appId: string;
    objectApiName: string;
    recordId: string;
  }): Promise<boolean> {
    const client = await this.dependencies.pool.connect();
    try {
      await client.query("begin");
      const result = await client.query(
        `update engine.record_change_log
         set action = 'sync'
         where mutation_id = $1 and action_request_id = $2 and org_id = $3
           and app_id = $4 and object_api_name = $5 and record_id = $6`,
        [
          input.mutationId,
          input.actionRequestId,
          input.orgId,
          input.appId,
          input.objectApiName,
          input.recordId,
        ],
      );
      if (result.rowCount === 1) {
        await client.query(
          `delete from engine.recycle_record
           where org_id = $1 and app_id = $2 and object_api_name = $3
             and record_id = $4 and deletion_action_request_id = $5`,
          [
            input.orgId,
            input.appId,
            input.objectApiName,
            input.recordId,
            input.actionRequestId,
          ],
        );
      }
      await client.query("commit");
      return result.rowCount === 1;
    } catch (error) {
      await client.query("rollback");
      throw error;
    } finally {
      client.release();
    }
  }

  private async targetState(
    object: RecordObject,
    id: string,
    token: string,
  ): Promise<"missing" | "live" | "deleted"> {
    const operationName = "RecordsSyncTargetState";
    const data = await this.dependencies.graphjin.execute<Record<string, unknown>>({
      operationName,
      query: `query ${operationName} { ${object.tableName}(where: { id: { eq: $id } }, limit: 1) { id ${RECORD_SYSTEM_COLUMNS.deletedAt} } }`,
      variables: { id },
      token,
    });
    const row = resultRows(data, object.tableName)[0];
    if (!row) return "missing";
    return row[RECORD_SYSTEM_COLUMNS.deletedAt] === null ? "live" : "deleted";
  }

  async execute(request: RecordWriteRequest): Promise<RecordWriteResult> {
    if (!request.actionRequestId.trim() || !request.orgId.trim() || !request.appId.trim()) {
      throw new RecordWriteTargetError("record write requires request, org, and app ids");
    }
    if (!request.actor.userId.trim()) {
      throw new RecordWriteTargetError("record write requires an acting user");
    }
    const sync = request.system?.kind === "sync";
    if (request.operation === "upsert" && !sync) {
      throw new RecordWriteTargetError("upsert is reserved for connector sync");
    }
    if (sync && !request.system?.sourceInstanceId.trim()) {
      throw new RecordWriteTargetError("record sync requires a source instance");
    }
    await this.dependencies.assertWritesAllowed?.();
    const { snapshot, object, fields, grant } = await this.resolve(request);
    const mode = await this.dependencies.appWriteMode?.(
      request.orgId,
      request.appId,
    );
    if (sync && mode !== "mirror" && mode !== "cutting_over") {
      throw new RecordSyncWriteBlockedError(mode ?? null);
    }
    if (!sync && (mode === "mirror" || mode === "cutting_over")) {
      throw new RecordMirrorWriteBlockedError(mode);
    }
    const rawFields = { ...(request.fields ?? {}) };
    const nominatedOwner =
      typeof rawFields[RECORD_SYSTEM_COLUMNS.ownerUserId] === "string"
        ? String(rawFields[RECORD_SYSTEM_COLUMNS.ownerUserId])
        : null;
    delete rawFields[RECORD_SYSTEM_COLUMNS.ownerUserId];
    if (
      nominatedOwner !== null &&
      request.operation !== "create" &&
      !(sync && request.operation === "upsert")
    ) {
      throw new RecordValidationError([
        {
          field: RECORD_SYSTEM_COLUMNS.ownerUserId,
          code: "read_only",
          message: "can only be selected during create",
        },
      ]);
    }
    if (object.visibility !== "owner" && nominatedOwner !== null) {
      throw new RecordValidationError([
        {
          field: RECORD_SYSTEM_COLUMNS.ownerUserId,
          code: "unknown",
          message: "is not valid for an org-visible object",
        },
      ]);
    }

    const ownerUserId =
      object.visibility === "owner"
        ? sync
          ? nominatedOwner
          : request.actor.role === "member"
          ? nominatedOwner ?? request.actor.userId
          : nominatedOwner ?? request.actor.userId
        : null;
    if (!sync) this.assertPermission(request, object, grant, ownerUserId);

    let validatedFields: Record<string, unknown> = {};
    let expectedFields: Record<string, unknown> = {};
    let references: RecordReferenceCheck[] = [];
    if (
      request.operation === "create" ||
      request.operation === "update" ||
      request.operation === "upsert"
    ) {
      const validated = validateRecordFields({
        values: rawFields,
        registryFields: fields,
        operation: request.operation === "upsert" ? "create" : request.operation,
        allowReadOnly: sync,
      });
      validatedFields = validated.fields;
      references = validated.references;
    } else if (Object.keys(rawFields).length > 0) {
      throw new RecordValidationError([
        { field: "$", code: "fields", message: "delete and restore do not accept fields" },
      ]);
    }
    if (request.expected !== undefined) {
      if (request.operation !== "update") {
        throw new RecordValidationError([
          { field: "expected", code: "operation", message: "is only valid for updates" },
        ]);
      }
      expectedFields = validateRecordFields({
        values: request.expected,
        registryFields: fields,
        operation: "expected",
      }).fields;
    }

    const id =
      request.operation === "create"
        ? String(validatedFields.id ?? deterministicRecordId(request.actionRequestId))
        : request.id?.trim() ?? "";
    if (!id || id.length > 512) {
      throw new RecordValidationError([
        { field: "id", code: "id", message: "must be a non-empty string" },
      ]);
    }
    delete validatedFields.id;
    const token = await this.dependencies.serviceToken(request.orgId);
    for (const reference of sync ? [] : references) {
      if (!(await this.referenceExists(snapshot, reference, token))) {
        throw new RecordReferenceMissingError(reference.fieldApiName);
      }
    }

    const actionKind = sync ? "record_sync" : `record_${request.operation}`;
    const claim = await claimRecordsActionExecution(this.dependencies.pool, {
      actionRequestId: request.actionRequestId,
      orgId: request.orgId,
      appId: request.appId,
      actionKind,
      leaseOwner: this.dependencies.leaseOwner,
    });
    if (claim.kind === "replay") {
      return { ...(claim.result as Omit<RecordWriteResult, "replayed">), replayed: true };
    }

    let operation: RecordWriteOperation =
      request.operation === "upsert" ? "update" : request.operation;
    let targetState: "missing" | "live" | "deleted" | null = null;
    if (sync) {
      const existingOperation = claim.execution.result?.sync_operation;
      if (
        existingOperation === "create" ||
        existingOperation === "update" ||
        existingOperation === "delete" ||
        existingOperation === "restore" ||
        existingOperation === "upsert"
      ) {
        operation = existingOperation === "upsert" ? "update" : existingOperation;
      } else {
        if (request.operation === "delete") {
          targetState = await this.targetState(object, id, token);
        }
        await setRecordsActionExecutionContext(this.dependencies.pool, {
          actionRequestId: request.actionRequestId,
          leaseOwner: this.dependencies.leaseOwner,
          context: { sync_operation: request.operation === "upsert" ? "upsert" : operation },
        });
      }
    }
    const mutationId = deterministicMutationId(
      request,
      request.operation === "upsert" ? "upsert" : operation,
      id,
    );

    const baseResult: RecordWriteResult = {
      actionRequestId: request.actionRequestId,
      appId: request.appId,
      objectApiName: object.apiName,
      tableName: object.tableName,
      id,
      operation,
      mutationId,
      replayed: false,
      recovered: false,
    };
    const auditIdentity = {
      mutationId,
      actionRequestId: request.actionRequestId,
      orgId: request.orgId,
      appId: request.appId,
      objectApiName: object.apiName,
      recordId: id,
    };
    try {
      if (
        await (sync
          ? this.normalizeSyncAudit(auditIdentity)
          : this.auditExists(auditIdentity))
      ) {
        const recovered = { ...baseResult, recovered: true };
        if (!sync) {
          await this.dependencies.recordSourceWrite({
            actionRequestId: request.actionRequestId,
            orgId: request.orgId,
            tableName: object.tableName,
            recordId: id,
          });
        }
        await succeedRecordsActionExecution(this.dependencies.pool, {
          actionRequestId: request.actionRequestId,
          leaseOwner: this.dependencies.leaseOwner,
          result: recovered,
        });
        return recovered;
      }
      if (
        sync &&
        request.operation === "delete" &&
        (targetState ?? (await this.targetState(object, id, token))) !== "live"
      ) {
        const noOp = { ...baseResult, noOp: true };
        await succeedRecordsActionExecution(this.dependencies.pool, {
          actionRequestId: request.actionRequestId,
          leaseOwner: this.dependencies.leaseOwner,
          result: noOp,
        });
        return noOp;
      }

      const now = (this.dependencies.now ?? (() => new Date()))().toISOString();
      const priorMutationId =
        operation === "update" && Object.keys(expectedFields).length > 0
          ? await this.checkExpected(object, id, expectedFields, token)
          : null;
      const where = buildMutationWhere({
        id,
        restore: operation === "restore",
        includeDeleted: sync && request.operation === "upsert",
        memberOwnerUserId:
          object.visibility === "owner" && request.actor.role === "member"
            ? request.actor.userId
            : null,
        priorMutationId,
      });

      let operationName: string;
      let query: string;
      let variables: Record<string, unknown>;
      if (operation === "create") {
        operationName = "RecordsCreate";
        query = `mutation ${operationName} { ${object.tableName}(insert: $data) { id } }`;
        variables = {
          data: {
            id,
            [RECORD_DATA_COLUMN]: validatedFields,
            ...(object.visibility === "owner"
              ? { [RECORD_SYSTEM_COLUMNS.ownerUserId]: ownerUserId }
              : {}),
            [RECORD_SYSTEM_COLUMNS.createdBy]: request.actor.userId,
            [RECORD_SYSTEM_COLUMNS.updatedBy]: request.actor.userId,
            [RECORD_SYSTEM_COLUMNS.actionRequestId]: request.actionRequestId,
            [RECORD_SYSTEM_COLUMNS.mutationId]: mutationId,
          },
        };
      } else {
        operationName =
          operation === "update"
            ? "RecordsUpdate"
            : operation === "delete"
              ? "RecordsDelete"
              : "RecordsRestore";
        query = `mutation ${operationName} { ${object.tableName}(update: $data, where: ${where.document}) { id } }`;
        variables = {
          data: {
            ...(Object.keys(validatedFields).length > 0
              ? { [RECORD_DATA_COLUMN]: validatedFields }
              : {}),
            ...(operation === "delete"
              ? { [RECORD_SYSTEM_COLUMNS.deletedAt]: now }
              : {}),
            ...(operation === "restore"
              ? { [RECORD_SYSTEM_COLUMNS.deletedAt]: null }
              : {}),
            ...(sync && request.operation === "upsert"
              ? { [RECORD_SYSTEM_COLUMNS.deletedAt]: null }
              : {}),
            [RECORD_SYSTEM_COLUMNS.updatedBy]: request.actor.userId,
            [RECORD_SYSTEM_COLUMNS.actionRequestId]: request.actionRequestId,
            [RECORD_SYSTEM_COLUMNS.mutationId]: mutationId,
          },
          ...where.variables,
        };
      }

      let data = await this.dependencies.graphjin.execute<Record<string, unknown>>({
        operationName,
        query,
        variables,
        token,
      });
      let rows = resultRows(data, object.tableName);
      let audited = await (sync
        ? this.normalizeSyncAudit(auditIdentity)
        : this.auditExists(auditIdentity));
      let returnedTarget = rows.length === 1 && rows[0]?.id === id;

      if (
        sync &&
        request.operation === "upsert" &&
        !audited &&
        !returnedTarget
      ) {
        operation = "create";
        baseResult.operation = "create";
        operationName = "RecordsSyncInsert";
        data = await this.dependencies.graphjin.execute<Record<string, unknown>>({
          operationName,
          query: `mutation ${operationName} { ${object.tableName}(insert: $data) { id } }`,
          variables: {
            data: {
              id,
              [RECORD_DATA_COLUMN]: validatedFields,
              ...(object.visibility === "owner"
                ? { [RECORD_SYSTEM_COLUMNS.ownerUserId]: ownerUserId }
                : {}),
              [RECORD_SYSTEM_COLUMNS.createdBy]: request.actor.userId,
              [RECORD_SYSTEM_COLUMNS.updatedBy]: request.actor.userId,
              [RECORD_SYSTEM_COLUMNS.actionRequestId]: request.actionRequestId,
              [RECORD_SYSTEM_COLUMNS.mutationId]: mutationId,
            },
          },
          token,
        });
        rows = resultRows(data, object.tableName);
        audited = await this.normalizeSyncAudit(auditIdentity);
        returnedTarget = rows.length === 1 && rows[0]?.id === id;
      }

      // GraphJin 3.18 applies a mutation's `where` expression again while
      // selecting its result. Updates that intentionally change an expected or
      // soft-delete value can therefore commit and still return an empty list.
      // The trigger-written audit row is the same-transaction commit proof.
      if (!audited && !returnedTarget) {
        if (operation === "update" && Object.keys(expectedFields).length > 0) {
          throw new RecordConcurrencyConflictError();
        }
        throw new RecordNotFoundOrDeniedError();
      }
      if (!audited) throw new RecordMutationAuditMissingError();

      if (!sync) {
        await this.dependencies.recordSourceWrite({
          actionRequestId: request.actionRequestId,
          orgId: request.orgId,
          tableName: object.tableName,
          recordId: id,
        });
      }
      await succeedRecordsActionExecution(this.dependencies.pool, {
        actionRequestId: request.actionRequestId,
        leaseOwner: this.dependencies.leaseOwner,
        result: baseResult,
      });
      return baseResult;
    } catch (error) {
      if (terminalError(error)) {
        await failRecordsActionExecution(this.dependencies.pool, {
          actionRequestId: request.actionRequestId,
          leaseOwner: this.dependencies.leaseOwner,
          error: durableError(error),
        }).catch(() => undefined);
      }
      throw error;
    }
  }
}
