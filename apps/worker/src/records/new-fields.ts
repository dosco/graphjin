import {
  RecordRegistry,
  validateRecordIdentifier,
  type PreparedRecordSchemaAction,
  type RecordFieldKind,
  type RecordSchemaActionRequest,
  type RecordSchemaChange,
} from "@neko/records";
import {
  updateActionRequestPayload,
  type ActionRequestCreatedHook,
  type ActionRequestRecord,
} from "@neko/llm/workflows";
import type pg from "pg";

/** A field a record write proposes because the object does not have it yet. */
export type RecordNewField = {
  api_name: string;
  label: string;
  kind: RecordFieldKind;
  picklist_values?: string[];
};

export class RecordNewFieldsError extends Error {
  readonly code = "records_new_fields_invalid";

  constructor(message: string) {
    super(message);
    this.name = "RecordNewFieldsError";
  }
}

const NEW_FIELD_KINDS = new Set(["record_create", "record_update"]);
const MAX_NEW_FIELDS = 20;

function labelFor(apiName: string): string {
  const words = apiName.replace(/_+/g, " ").trim();
  return words ? words[0]!.toUpperCase() + words.slice(1) : apiName;
}

/** Infer the narrowest field type that holds the value as written. */
export function inferFieldKind(value: unknown): { kind: RecordFieldKind; picklistValues?: string[] } {
  if (typeof value === "boolean") return { kind: "boolean" };
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new RecordNewFieldsError("new number fields need a finite value");
    return { kind: Number.isSafeInteger(value) ? "integer" : "decimal" };
  }
  if (Array.isArray(value)) {
    const choices = [...new Set(value)];
    if (choices.length === 0 || choices.some((choice) => typeof choice !== "string" || !choice.trim())) {
      throw new RecordNewFieldsError("new list fields need a list of text choices");
    }
    return { kind: "multipicklist", picklistValues: choices as string[] };
  }
  if (typeof value !== "string") {
    throw new RecordNewFieldsError("new fields hold text, numbers, true or false, dates, or lists of choices");
  }
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) return { kind: "date" };
  if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/.test(value) && !Number.isNaN(Date.parse(value))) {
    return { kind: "datetime" };
  }
  if (/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) return { kind: "email" };
  if (/^https?:\/\/\S+$/i.test(value)) return { kind: "url" };
  if (value.length > 255 || value.includes("\n")) return { kind: "textarea" };
  return { kind: "text" };
}

/** The keys of a record write that the object does not define yet. */
export async function proposeRecordNewFields(
  registry: Pick<RecordRegistry, "loadApp">,
  request: Pick<ActionRequestRecord, "orgId" | "payload">,
): Promise<RecordNewField[]> {
  const payload = request.payload;
  const fields = payload.fields;
  if (typeof payload.app !== "string" || typeof payload.object !== "string") return [];
  if (!fields || typeof fields !== "object" || Array.isArray(fields)) return [];
  const snapshot = await registry.loadApp(request.orgId, payload.app);
  const object = snapshot?.objects.find(
    (candidate) => candidate.apiName === payload.object && candidate.archivedAt === null,
  );
  if (!snapshot || !object) return [];
  const known = new Map(
    snapshot.fields
      .filter((field) => field.objectId === object.id)
      .map((field) => [String(field.apiName), field]),
  );
  const proposed: RecordNewField[] = [];
  for (const [key, value] of Object.entries(fields as Record<string, unknown>)) {
    if (key === "id") continue;
    const existing = known.get(key);
    if (existing && existing.archivedAt === null) continue;
    if (existing) {
      throw new RecordNewFieldsError(`field ${key} is archived; restore it before writing to it`);
    }
    let apiName: string;
    try {
      apiName = validateRecordIdentifier(key);
    } catch {
      throw new RecordNewFieldsError(
        `field name ${JSON.stringify(key)} must use lowercase letters, digits and underscores`,
      );
    }
    if (value === null) {
      throw new RecordNewFieldsError(`new field ${key} needs a value so its type can be set`);
    }
    const inferred = inferFieldKind(value);
    proposed.push({
      api_name: apiName,
      label: labelFor(apiName),
      kind: inferred.kind,
      ...(inferred.picklistValues ? { picklist_values: inferred.picklistValues } : {}),
    });
  }
  if (proposed.length > MAX_NEW_FIELDS) {
    throw new RecordNewFieldsError(`a record write can add at most ${MAX_NEW_FIELDS} new fields`);
  }
  return proposed;
}

/**
 * Before a record write is shown for approval, list the fields it would add
 * so the approver sees and approves them together with the write.
 */
export function createRecordNewFieldsPreflightHook(
  registry: Pick<RecordRegistry, "loadApp">,
  updatePayload: typeof updateActionRequestPayload = updateActionRequestPayload,
): ActionRequestCreatedHook {
  return async (request) => {
    if (!NEW_FIELD_KINDS.has(request.kind)) return;
    const proposed = await proposeRecordNewFields(registry, request);
    if (proposed.length === 0) return;
    return updatePayload({
      id: request.id,
      orgId: request.orgId,
      payload: { ...request.payload, new_fields: proposed },
    });
  };
}

export type RecordNewFieldsSchema = {
  planner: { prepare(request: RecordSchemaActionRequest): Promise<PreparedRecordSchemaAction> };
  saga: {
    get(actionRequestId: string): Promise<RecordSchemaChange | null>;
    approve(actionRequestId: string, previewHash: string): Promise<RecordSchemaChange>;
    execute(actionRequestId: string): Promise<RecordSchemaChange>;
  };
};

function newFieldsFrom(payload: Record<string, unknown>): RecordNewField[] {
  const value = payload.new_fields;
  if (value === undefined) return [];
  if (!Array.isArray(value)) throw new RecordNewFieldsError("new_fields must be a list");
  return value.map((entry) => {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
      throw new RecordNewFieldsError("each new field must be an object");
    }
    const field = entry as Record<string, unknown>;
    if (typeof field.api_name !== "string" || typeof field.label !== "string" || typeof field.kind !== "string") {
      throw new RecordNewFieldsError("each new field needs api_name, label and kind");
    }
    return field as RecordNewField;
  });
}

/**
 * Add the approved new fields through the governed schema path, so they
 * appear in schema history and get the object's group access, then let the
 * record write run. Each field has its own journal entry keyed by the
 * record action, which makes retries resume instead of repeat.
 */
export async function applyRecordNewFields(input: {
  request: ActionRequestRecord;
  schema: RecordNewFieldsSchema;
  approverIsAdmin: (request: ActionRequestRecord) => Promise<boolean>;
}): Promise<string[]> {
  const fields = newFieldsFrom(input.request.payload);
  if (fields.length === 0) return [];
  if (!(await input.approverIsAdmin(input.request))) {
    throw new RecordNewFieldsError("adding fields needs approval from an administrator");
  }
  const app = String(input.request.payload.app);
  const object = String(input.request.payload.object);
  const added: string[] = [];
  for (const field of fields) {
    const journalId = `${input.request.id}:field:${field.api_name}`;
    let change = await input.schema.saga.get(journalId);
    if (!change) {
      const prepared = await input.schema.planner.prepare({
        id: journalId,
        orgId: input.request.orgId,
        kind: "app_field_add",
        payload: { app, object, field: { ...field, required: false } },
        actorUserId: input.request.approvedByUserId,
        actorRole: "admin",
      });
      change = prepared.change;
    }
    if (change.phase === "planned") {
      change = await input.schema.saga.approve(journalId, change.previewHash);
    }
    if (change.phase !== "projected") {
      await input.schema.saga.execute(journalId);
    }
    added.push(field.api_name);
  }
  return added;
}

/** The approver of a field-adding write must be an administrator. */
export async function approverIsAdmin(request: ActionRequestRecord): Promise<boolean> {
  const { app_user, db, eq, resolveUserGroups } = await import("@neko/db");
  const approver = request.approvedByUserId?.trim() ?? "";
  if (!approver) {
    // Solo installs have no users; the operator is the administrator.
    const [anyUser] = await db()
      .select({ id: app_user.id })
      .from(app_user)
      .where(eq(app_user.org_id, request.orgId))
      .limit(1);
    return !anyUser;
  }
  const groups = await resolveUserGroups(request.orgId, approver);
  return groups.administrator;
}

export function recordNewFieldsRegistry(pool: pg.Pool): RecordRegistry {
  return new RecordRegistry(pool);
}
