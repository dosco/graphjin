import type {
  RecordPolicyActor,
  RecordWriteOperation,
  RecordWriteRequest,
  RecordWriteResult,
} from "@neko/records";
import {
  RecordMutationAuditMissingError,
  RecordsActionLeaseBusyError,
  RecordsActionLeaseLostError,
  RecordsGraphjinRequestError,
} from "@neko/records";
import {
  appendWorkflowRunSourceWrite,
  getActionRequest,
  registerActionAdapter,
  RetryableActionAdapterError,
  type ActionAdapter,
  type ActionRequestRecord,
} from "@neko/llm/workflows";
import { applyRecordNewFields, type RecordNewFieldsSchema } from "./new-fields.js";
import {
  RECORD_SCHEMA_ACTION_DESCRIPTORS,
  RECORD_SCHEMA_ACTION_KINDS,
} from "./schema-adapters.js";
import {
  RECORD_IMPORT_ACTION_DESCRIPTORS,
  RECORD_IMPORT_ACTION_KINDS,
} from "./import-adapters.js";
import {
  RECORD_IDENTITY_ACTION_DESCRIPTORS,
  RECORD_IDENTITY_ACTION_KINDS,
} from "./identity-adapters.js";
import {
  RECORD_SALESFORCE_ACTION_DESCRIPTORS,
  RECORD_SALESFORCE_ACTION_KINDS,
} from "./salesforce-adapters.js";
import {
  RECORD_ARTIFACT_IMPORT_ACTION_DESCRIPTORS,
  RECORD_ARTIFACT_IMPORT_ACTION_KINDS,
} from "./artifact-import-adapters.js";
import {
  RECORD_BACKFILL_ACTION_DESCRIPTORS,
  RECORD_BACKFILL_ACTION_KINDS,
} from "./backfill-adapters.js";
import {
  RECORD_ACCESS_ACTION_DESCRIPTORS,
  RECORD_ACCESS_ACTION_KINDS,
} from "./access-adapters.js";

export const RECORD_ACTION_KINDS = [
  "record_create",
  "record_update",
  "record_delete",
  "record_restore",
] as const;

export type RecordActionKind = (typeof RECORD_ACTION_KINDS)[number];

export const RECORD_ACTION_DESCRIPTORS = [
  {
    kind: "record_create",
    scope: "internal",
    description:
      "Create one record in an OpenNeko app through registry validation, app RBAC, and durable audit history. A field name the object does not have yet becomes a proposed new field, typed from its value; an administrator approves the fields with the write.",
    default_mode: "ask",
    example: {
      app: "crm",
      object: "contact",
      fields: {
        lastname: "Rivera",
        accountid: "0015g00000XYZ",
      },
    },
  },
  {
    kind: "record_update",
    scope: "internal",
    description:
      "Update one OpenNeko app record; include expected field values when the change depends on what was read. Only the sent fields change. A field name the object does not have yet becomes a proposed new field that an administrator approves with the write.",
    default_mode: "ask",
    example: {
      app: "crm",
      object: "opportunity",
      id: "0065g00000ABCDEAA4",
      fields: { stagename: "Negotiation", closedate: "2026-08-15" },
      expected: { stagename: "Proposal" },
    },
  },
  {
    kind: "record_delete",
    scope: "internal",
    description:
      "Soft-delete one OpenNeko app record into its recycle bin; no data is physically dropped.",
    default_mode: "ask",
    example: {
      app: "crm",
      object: "contact",
      id: "0035g00000QRSTU",
    },
  },
  {
    kind: "record_restore",
    scope: "internal",
    description: "Restore one soft-deleted OpenNeko app record from its recycle bin.",
    default_mode: "ask",
    example: {
      app: "crm",
      object: "contact",
      id: "0035g00000QRSTU",
    },
  },
] as const;

export type WorkerActionDescriptor = {
  kind: string;
  description: string;
  /** Fixed policy scope. Plugin actions default to external when omitted. */
  scope?: "external" | "internal";
  default_mode?:
    | "auto"
    | "ask"
    | "deny"
    | {
        external?: "auto" | "ask" | "deny";
        internal?: "auto" | "ask" | "deny";
      };
  example?: Record<string, unknown>;
};

export function includeRecordActionDescriptors(
  descriptors: readonly WorkerActionDescriptor[],
): WorkerActionDescriptor[] {
  const builtins = new Set<string>([
    ...RECORD_ACTION_KINDS,
    ...RECORD_BACKFILL_ACTION_KINDS,
    ...RECORD_SCHEMA_ACTION_KINDS,
    ...RECORD_IMPORT_ACTION_KINDS,
    ...RECORD_IDENTITY_ACTION_KINDS,
    ...RECORD_SALESFORCE_ACTION_KINDS,
    ...RECORD_ARTIFACT_IMPORT_ACTION_KINDS,
    ...RECORD_ACCESS_ACTION_KINDS,
  ]);
  return [
    ...RECORD_ACTION_DESCRIPTORS,
    ...RECORD_BACKFILL_ACTION_DESCRIPTORS,
    ...RECORD_SCHEMA_ACTION_DESCRIPTORS,
    ...RECORD_IMPORT_ACTION_DESCRIPTORS,
    ...RECORD_IDENTITY_ACTION_DESCRIPTORS,
    ...RECORD_SALESFORCE_ACTION_DESCRIPTORS,
    ...RECORD_ARTIFACT_IMPORT_ACTION_DESCRIPTORS,
    ...RECORD_ACCESS_ACTION_DESCRIPTORS,
    ...descriptors.filter((descriptor) => !builtins.has(descriptor.kind)),
  ];
}

export class RecordActionPayloadError extends Error {
  readonly code = "records_action_payload_invalid";

  constructor(message: string) {
    super(message);
    this.name = "RecordActionPayloadError";
  }
}

export type RecordActionExecutor = {
  execute(request: RecordWriteRequest): Promise<RecordWriteResult>;
};

export type RecordActionActorResolver = (
  request: ActionRequestRecord,
) => Promise<RecordPolicyActor>;

const TRANSIENT_ERROR_CODES = new Set([
  "ECONNREFUSED",
  "ECONNRESET",
  "EPIPE",
  "ETIMEDOUT",
  "53300",
  "53400",
  "57P01",
  "57P02",
  "57P03",
]);

function isRetryableRecordsError(error: unknown): boolean {
  if (
    error instanceof RecordsActionLeaseBusyError ||
    error instanceof RecordsActionLeaseLostError ||
    error instanceof RecordMutationAuditMissingError
  ) {
    return true;
  }
  if (error instanceof RecordsGraphjinRequestError) {
    return (
      error.status === null ||
      error.status === 429 ||
      error.status >= 500 ||
      (error.status === 200 && error.graphjinErrors.length === 0)
    );
  }
  const code =
    typeof error === "object" && error !== null && "code" in error
      ? String((error as { code: unknown }).code)
      : "";
  return code.startsWith("08") || TRANSIENT_ERROR_CODES.has(code);
}

export async function recordActionRequestSourceWrite(input: {
  actionRequestId: string;
  orgId: string;
  tableName: string;
  recordId: string;
}): Promise<void> {
  const request = await getActionRequest(input.orgId, input.actionRequestId);
  if (!request) {
    throw new Error(
      `action_request ${input.actionRequestId} disappeared before source-write recording`,
    );
  }
  if (!request.workflowRunId) return;
  const recorded = await appendWorkflowRunSourceWrite({
    orgId: input.orgId,
    workflowRunId: request.workflowRunId,
    table: input.tableName,
    primaryKey: { id: input.recordId },
  });
  if (!recorded) {
    throw new Error(
      `workflow_run ${request.workflowRunId} disappeared before source-write recording`,
    );
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requiredString(
  payload: Record<string, unknown>,
  field: string,
): string {
  const value = payload[field];
  if (typeof value !== "string" || !value.trim()) {
    throw new RecordActionPayloadError(
      `${field}: a non-empty string is required`,
    );
  }
  return value.trim();
}

function requiredRecord(
  payload: Record<string, unknown>,
  field: string,
): Record<string, unknown> {
  const value = payload[field];
  if (!isRecord(value)) {
    throw new RecordActionPayloadError(`${field}: an object is required`);
  }
  return { ...value };
}

function optionalRecord(
  payload: Record<string, unknown>,
  field: string,
): Record<string, unknown> | undefined {
  if (payload[field] === undefined) return undefined;
  return requiredRecord(payload, field);
}

function assertOnlyKeys(
  payload: Record<string, unknown>,
  allowed: readonly string[],
): void {
  const allowlist = new Set(allowed);
  const unknown = Object.keys(payload).filter((key) => !allowlist.has(key));
  if (unknown.length > 0) {
    throw new RecordActionPayloadError(
      `unknown payload field${unknown.length === 1 ? "" : "s"}: ${unknown.sort().join(", ")}`,
    );
  }
}

function operationForKind(kind: RecordActionKind): RecordWriteOperation {
  switch (kind) {
    case "record_create":
      return "create";
    case "record_update":
      return "update";
    case "record_delete":
      return "delete";
    case "record_restore":
      return "restore";
  }
}

async function actorForRequest(request: ActionRequestRecord): Promise<RecordPolicyActor> {
  if (request.actorRole !== "admin" && request.actorRole !== "member") {
    throw new RecordActionPayloadError(
      "record actions require an admin or member actor snapshot",
    );
  }
  const actorUserId = request.actorUserId?.trim() ?? "";
  if (request.actorRole === "member" && !actorUserId) {
    throw new RecordActionPayloadError(
      "record actions require a linked member identity",
    );
  }
  const {
    and,
    app_user,
    db,
    eq,
    resolveUserGroups,
  } = await import("@neko/db");
  if (!actorUserId) {
    const [anyUser] = await db()
      .select({ id: app_user.id })
      .from(app_user)
      .where(eq(app_user.org_id, request.orgId))
      .limit(1);
    if (anyUser || request.actorRole !== "admin") {
      throw new RecordActionPayloadError(
        "record action actor is no longer available",
      );
    }
    return {
      userId: `urn:openneko:solo-admin:${request.orgId}`,
      role: "admin",
      groupIds: [],
      solo: true,
    };
  }
  const [user] = await db()
    .select({ disabledAt: app_user.disabled_at })
    .from(app_user)
    .where(
      and(eq(app_user.org_id, request.orgId), eq(app_user.id, actorUserId)),
    )
    .limit(1);
  if (!user || user.disabledAt) {
    throw new RecordActionPayloadError(
      "record action actor is disabled or no longer belongs to the organization",
    );
  }
  const groups = await resolveUserGroups(request.orgId, actorUserId);
  return {
    userId: actorUserId,
    role: groups.administrator ? "admin" : "member",
    groupIds: [...groups.groupIds, ...groups.ssoGroupIds],
    solo: false,
  };
}

async function writeRequest(
  kind: RecordActionKind,
  request: ActionRequestRecord,
  resolveActor: RecordActionActorResolver,
): Promise<RecordWriteRequest> {
  const payload = request.payload;
  const operation = operationForKind(kind);
  if (kind === "record_create") {
    assertOnlyKeys(payload, ["app", "object", "fields", "new_fields"]);
  } else if (kind === "record_update") {
    assertOnlyKeys(payload, ["app", "object", "id", "fields", "expected", "new_fields"]);
  } else {
    assertOnlyKeys(payload, ["app", "object", "id"]);
  }
  const common = {
    actionRequestId: request.id,
    orgId: request.orgId,
    appId: requiredString(payload, "app"),
    objectApiName: requiredString(payload, "object"),
    operation,
    actor: await resolveActor(request),
  };

  if (kind === "record_create") {
    return { ...common, fields: requiredRecord(payload, "fields") };
  }
  if (kind === "record_update") {
    return {
      ...common,
      id: requiredString(payload, "id"),
      fields: requiredRecord(payload, "fields"),
      expected: optionalRecord(payload, "expected"),
    };
  }
  return { ...common, id: requiredString(payload, "id") };
}

export type RecordNewFieldsSupport = {
  schema: RecordNewFieldsSchema;
  approverIsAdmin: (request: ActionRequestRecord) => Promise<boolean>;
};

export function createRecordActionAdapter(
  kind: RecordActionKind,
  executor: RecordActionExecutor,
  resolveActor: RecordActionActorResolver = actorForRequest,
  newFields?: RecordNewFieldsSupport,
): ActionAdapter {
  return async ({ request }) => {
    if (request.kind !== kind) {
      throw new RecordActionPayloadError(
        `adapter ${kind} cannot execute action kind ${request.kind}`,
      );
    }
    let result: RecordWriteResult;
    try {
      if (request.payload.new_fields !== undefined) {
        if (!newFields) {
          throw new RecordActionPayloadError("this worker cannot add new record fields");
        }
        await applyRecordNewFields({ request, ...newFields });
      }
      result = await executor.execute(
        await writeRequest(kind, request, resolveActor),
      );
    } catch (error) {
      if (isRetryableRecordsError(error)) {
        throw new RetryableActionAdapterError(
          `records action ${request.id} is temporarily unavailable and will be retried`,
          { cause: error },
        );
      }
      throw error;
    }
    return {
      commandOrOperation: kind,
      externalRef: `${result.tableName}:${result.id}`,
      result: { ...result },
    };
  };
}

export function registerRecordActionAdapters(
  executor: RecordActionExecutor,
  register: (kind: string, adapter: ActionAdapter) => void = registerActionAdapter,
  resolveActor: RecordActionActorResolver = actorForRequest,
  newFields?: RecordNewFieldsSupport,
): void {
  for (const kind of RECORD_ACTION_KINDS) {
    register(kind, createRecordActionAdapter(kind, executor, resolveActor, newFields));
  }
}
