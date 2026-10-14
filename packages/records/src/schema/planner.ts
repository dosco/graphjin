import type { Pool } from "pg";
import { recordIdentifier } from "../naming";
import { RecordRegistry } from "../registry";
import {
  RECORD_FIELD_KINDS,
  type JsonObject,
  type RecordFieldKind,
  type RecordIdentifier,
} from "../types";
import { loadRecordsCatalogSnapshot } from "./catalog";
import {
  parseRecordAppDefinition,
  recordAppDefinitionDdlObjects,
  recordAppDefinitionFromSnapshot,
  type RecordAppDefinition,
} from "./definition";
import type { RecordDdlObjectDefinition } from "./ddl";
import {
  RECORD_SCHEMA_ACTIONS,
  type RecordSchemaAction,
  type RecordSchemaChange,
} from "./saga";

export type RecordSchemaActionRequest = {
  id: string;
  orgId: string;
  kind: string;
  payload: Record<string, unknown>;
  actorUserId: string | null;
  actorRole: string | null;
};

export type RecordSchemaPlanPreview = {
  version: 1;
  action: RecordSchemaAction;
  app: string;
  desiredRevision: string;
  summary: string;
  objects: Array<{
    apiName: string;
    label: string;
    fields: Array<{
      apiName: string;
      label: string;
      kind: string;
      required: boolean;
      archived: boolean;
      references: string[];
    }>;
    archived: boolean;
  }>;
  expectedEffects: string[];
  warnings: string[];
};

export type PreparedRecordSchemaAction = {
  change: RecordSchemaChange;
  payload: Record<string, unknown>;
  preview: RecordSchemaPlanPreview;
};

type PlanInput = {
  orgId: string;
  appId: string;
  actionRequestId: string;
  desiredRevision: string;
  action: RecordSchemaAction;
  actorUserId: string | null;
  detail: Record<string, unknown>;
  objects: RecordDdlObjectDefinition[];
};

export type RecordSchemaPlanStore = {
  plan(input: PlanInput): Promise<RecordSchemaChange>;
};

export class RecordSchemaActionPayloadError extends Error {
  readonly code = "records_schema_action_payload_invalid";

  constructor(message: string) {
    super(message);
    this.name = "RecordSchemaActionPayloadError";
  }
}

export class RecordSchemaCounterproposalError extends Error {
  readonly code = "records_schema_counterproposal_required";

  constructor(
    message: string,
    readonly counterproposal: Record<string, unknown>,
  ) {
    super(message);
    this.name = "RecordSchemaCounterproposalError";
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requiredRecord(
  payload: Record<string, unknown>,
  field: string,
): Record<string, unknown> {
  const value = payload[field];
  if (!isRecord(value)) {
    throw new RecordSchemaActionPayloadError(`${field}: an object is required`);
  }
  return { ...value };
}

function requiredString(
  payload: Record<string, unknown>,
  field: string,
): string {
  const value = payload[field];
  if (typeof value !== "string" || !value.trim()) {
    throw new RecordSchemaActionPayloadError(
      `${field}: a non-empty string is required`,
    );
  }
  return value.trim();
}

function assertOnlyKeys(
  payload: Record<string, unknown>,
  allowed: readonly string[],
  path = "payload",
): void {
  const allowlist = new Set(allowed);
  const unknown = Object.keys(payload).filter((key) => !allowlist.has(key));
  if (unknown.length > 0) {
    throw new RecordSchemaActionPayloadError(
      `${path}: unknown field${unknown.length === 1 ? "" : "s"}: ${unknown
        .sort()
        .join(", ")}`,
    );
  }
}

export function recordAppDefinitionPayload(
  definition: RecordAppDefinition,
): Record<string, unknown> {
  return {
    app: definition.appId,
    label: definition.label,
    purpose: definition.purpose,
    nav_order: definition.navOrder,
    objects: definition.objects.map((object) => ({
      api_name: object.apiName,
      source_api_name: object.sourceApiName,
      label: object.label,
      plural_label: object.pluralLabel,
      name_field: object.nameField,
      visibility: object.visibility,
      custom: object.custom,
      archived: object.archived,
      fields: object.fields.map((field) => ({
        api_name: field.apiName,
        source_api_name: field.sourceApiName,
        label: field.label,
        kind: field.kind,
        required: field.required,
        read_only: field.readOnly,
        archived: field.archived,
        picklist_values: field.picklistValues,
        reference_targets: field.referenceTargets,
        length: field.length,
        scale: field.scale,
      })),
      layouts: object.layouts.map((layout) => ({
        kind: layout.kind,
        definition: layout.definition,
      })),
    })),
    permissions: definition.permissions.map((permission) => ({
      role: permission.role,
      object: permission.objectApiName,
      read: permission.canRead,
      create: permission.canCreate,
      update: permission.canUpdate,
      delete: permission.canDelete,
    })),
    pages: definition.pages.map((page) => ({
      api_name: page.apiName,
      label: page.label,
      definition: page.definition,
      nav_order: page.navOrder,
    })),
  };
}

function objectPayload(
  payload: Record<string, unknown>,
  apiName: RecordIdentifier,
): Record<string, unknown> {
  const objects = payload.objects;
  if (!Array.isArray(objects)) throw new Error("canonical app objects are missing");
  const object = objects.find(
    (candidate) =>
      isRecord(candidate) && candidate.api_name === apiName,
  );
  if (!isRecord(object)) {
    throw new RecordSchemaActionPayloadError(`unknown record object: ${apiName}`);
  }
  return object;
}

function fieldsPayload(object: Record<string, unknown>): Record<string, unknown>[] {
  if (!Array.isArray(object.fields)) throw new Error("canonical object fields are missing");
  if (!object.fields.every(isRecord)) throw new Error("canonical object fields are invalid");
  return object.fields as Record<string, unknown>[];
}

function parseCurrent(payload: Record<string, unknown>): RecordAppDefinition {
  return parseRecordAppDefinition(payload);
}

function previewFor(
  action: RecordSchemaAction,
  definition: RecordAppDefinition,
  desiredRevision: string,
  effects: string[],
  warnings: string[],
): RecordSchemaPlanPreview {
  return {
    version: 1,
    action,
    app: definition.appId,
    desiredRevision,
    summary: `${action} will update ${definition.label}`,
    objects: definition.objects.map((object) => ({
      apiName: object.apiName,
      label: object.label,
      archived: object.archived,
      fields: object.fields.map((field) => ({
        apiName: field.apiName,
        label: field.label,
        kind: field.kind,
        required: field.required,
        archived: field.archived,
        references: field.referenceTargets ?? [],
      })),
    })),
    expectedEffects: effects,
    warnings,
  };
}

async function ddlMatchingPhysicalNullability(
  pool: Pool,
  definition: RecordAppDefinition,
): Promise<RecordDdlObjectDefinition[]> {
  const desired = recordAppDefinitionDdlObjects(definition);
  const catalog = await loadRecordsCatalogSnapshot(pool);
  const nullability = new Map<string, boolean>();
  for (const column of catalog.columns) {
    if (column.schema === "public") {
      nullability.set(`${column.table}\u0000${column.column}`, column.notNull);
    }
  }
  return desired.map((object) => ({
    ...object,
    fields: object.fields.map((field) => ({
      ...field,
      required:
        nullability.get(`${object.tableName}\u0000${field.columnName}`) ??
        field.required,
    })),
  }));
}

function mutateObjectCreate(
  current: RecordAppDefinition,
  payload: Record<string, unknown>,
): { definition: RecordAppDefinition; effects: string[]; warnings: string[] } {
  assertOnlyKeys(payload, ["app", "object"]);
  const canonical = recordAppDefinitionPayload(current);
  const objects = canonical.objects as unknown[];
  objects.push(requiredRecord(payload, "object"));
  const provisional = parseCurrent(canonical);
  const parsedObject = provisional.objects.find(
    (object) => !current.objects.some((existing) => existing.apiName === object.apiName),
  );
  if (!parsedObject) {
    throw new RecordSchemaActionPayloadError("object: a new permanent API name is required");
  }
  const permissions = canonical.permissions as unknown[];
  permissions.push(
    {
      role: "admin",
      object: parsedObject.apiName,
      read: true,
      create: true,
      update: true,
      delete: true,
    },
    {
      role: "member",
      object: parsedObject.apiName,
      read: true,
      create: false,
      update: false,
      delete: false,
    },
  );
  return {
    definition: parseCurrent(canonical),
    effects: [
      `Create object ${parsedObject.apiName} with ${parsedObject.fields.length} fields`,
      "Regenerate exhaustive GraphJin access policy",
    ],
    warnings: [],
  };
}

function mutateFieldAdd(
  current: RecordAppDefinition,
  payload: Record<string, unknown>,
): { definition: RecordAppDefinition; effects: string[]; warnings: string[] } {
  assertOnlyKeys(payload, ["app", "object", "field"]);
  const objectApiName = recordIdentifier(requiredString(payload, "object"));
  const canonical = recordAppDefinitionPayload(current);
  const object = objectPayload(canonical, objectApiName);
  const field = requiredRecord(payload, "field");
  fieldsPayload(object).push(field);
  const definition = parseCurrent(canonical);
  const added = definition.objects
    .find((candidate) => candidate.apiName === objectApiName)!
    .fields.at(-1)!;
  if (added.required) {
    throw new RecordSchemaCounterproposalError(
      "required fields cannot be added to an existing object without a backfill",
      {
        sequence: [
          {
            kind: "app_field_add",
            app: current.appId,
            object: objectApiName,
            field: { ...field, required: false },
          },
          {
            kind: "record_backfill",
            app: current.appId,
            object: objectApiName,
            to: added.apiName,
            requires_value: {
              kind: added.kind,
              prompt: `Choose the value for existing ${objectApiName} records`,
            },
          },
          {
            kind: "app_field_modify",
            app: current.appId,
            object: objectApiName,
            field: added.apiName,
            changes: { required: true },
          },
        ],
      },
    );
  }
  return {
    definition,
    effects: [
      `Add optional ${added.kind} field ${objectApiName}.${added.apiName}`,
      "Regenerate exhaustive GraphJin access policy",
    ],
    warnings: [],
  };
}

function suggestedBackfillTransform(
  kind: RecordFieldKind,
):
  | "copy"
  | "string"
  | "integer"
  | "decimal"
  | "boolean"
  | "date"
  | "datetime" {
  switch (kind) {
    case "integer":
      return "integer";
    case "decimal":
    case "currency":
    case "percent":
      return "decimal";
    case "boolean":
      return "boolean";
    case "date":
      return "date";
    case "datetime":
      return "datetime";
    case "text":
    case "textarea":
    case "email":
    case "phone":
    case "url":
    case "picklist":
    case "reference":
    case "readonly_formula":
      return "string";
    case "id":
    case "multipicklist":
      return "copy";
  }
}

function mutateFieldModify(
  current: RecordAppDefinition,
  payload: Record<string, unknown>,
): { definition: RecordAppDefinition; effects: string[]; warnings: string[] } {
  assertOnlyKeys(payload, ["app", "object", "field", "changes"]);
  const objectApiName = recordIdentifier(requiredString(payload, "object"));
  const fieldApiName = recordIdentifier(requiredString(payload, "field"));
  const changes = requiredRecord(payload, "changes");
  assertOnlyKeys(
    changes,
    ["label", "kind", "required", "read_only", "picklist_values", "reference_targets"],
    "payload.changes",
  );
  const canonical = recordAppDefinitionPayload(current);
  const object = objectPayload(canonical, objectApiName);
  const field = fieldsPayload(object).find(
    (candidate) => candidate.api_name === fieldApiName,
  );
  if (!field) {
    throw new RecordSchemaActionPayloadError(
      `unknown record field: ${objectApiName}.${fieldApiName}`,
    );
  }
  const oldKind = field.kind;
  if (changes.kind !== undefined && changes.kind !== oldKind) {
    if (
      typeof changes.kind !== "string" ||
      !RECORD_FIELD_KINDS.includes(changes.kind as RecordFieldKind) ||
      changes.kind === "id"
    ) {
      throw new RecordSchemaActionPayloadError(
        "payload.changes.kind: unsupported field kind",
      );
    }
    const targetKind = changes.kind as RecordFieldKind;
    const replacementApiName = recordIdentifier(`${fieldApiName}_v2`);
    const desiredRequired = changes.required ?? field.required;
    const desiredReadOnly = changes.read_only ?? field.read_only;
    throw new RecordSchemaCounterproposalError(
      "record field types are permanent; use an additive replacement and backfill",
      {
        sequence: [
          {
            kind: "app_field_add",
            app: current.appId,
            object: objectApiName,
            field: {
              ...field,
              api_name: replacementApiName,
              ...changes,
              required: false,
              read_only: false,
            },
          },
          {
            kind: "record_backfill",
            app: current.appId,
            object: objectApiName,
            from: fieldApiName,
            to: replacementApiName,
            transform: suggestedBackfillTransform(targetKind),
          },
          ...(desiredRequired || desiredReadOnly
            ? [
                {
                  kind: "app_field_modify",
                  app: current.appId,
                  object: objectApiName,
                  field: replacementApiName,
                  changes: {
                    ...(desiredRequired ? { required: true } : {}),
                    ...(desiredReadOnly ? { read_only: true } : {}),
                  },
                },
              ]
            : []),
          {
            kind: "app_field_archive",
            app: current.appId,
            object: objectApiName,
            field: fieldApiName,
          },
        ],
      },
    );
  }
  Object.assign(field, changes);
  const definition = parseCurrent(canonical);
  return {
    definition,
    effects: [`Update validation metadata for ${objectApiName}.${fieldApiName}`],
    warnings: ["Existing stored values are retained unchanged"],
  };
}

function mutateObjectArchive(
  current: RecordAppDefinition,
  payload: Record<string, unknown>,
): { definition: RecordAppDefinition; effects: string[]; warnings: string[] } {
  assertOnlyKeys(payload, ["app", "object"]);
  const objectApiName = recordIdentifier(requiredString(payload, "object"));
  const canonical = recordAppDefinitionPayload(current);
  objectPayload(canonical, objectApiName).archived = true;
  return {
    definition: parseCurrent(canonical),
    effects: [`Hide object ${objectApiName} from the app and GraphJin policy`],
    warnings: ["The physical table and every stored row will be retained"],
  };
}

function mutateFieldArchive(
  current: RecordAppDefinition,
  payload: Record<string, unknown>,
): { definition: RecordAppDefinition; effects: string[]; warnings: string[] } {
  assertOnlyKeys(payload, ["app", "object", "field", "replacement_name_field"]);
  const objectApiName = recordIdentifier(requiredString(payload, "object"));
  const fieldApiName = recordIdentifier(requiredString(payload, "field"));
  const canonical = recordAppDefinitionPayload(current);
  const object = objectPayload(canonical, objectApiName);
  const field = fieldsPayload(object).find(
    (candidate) => candidate.api_name === fieldApiName,
  );
  if (!field) {
    throw new RecordSchemaActionPayloadError(
      `unknown record field: ${objectApiName}.${fieldApiName}`,
    );
  }
  field.archived = true;
  if (object.name_field === fieldApiName) {
    if (payload.replacement_name_field === undefined) {
      throw new RecordSchemaActionPayloadError(
        "replacement_name_field is required when archiving an object's name field",
      );
    }
    object.name_field = recordIdentifier(
      requiredString(payload, "replacement_name_field"),
    );
  }
  return {
    definition: parseCurrent(canonical),
    effects: [`Hide field ${objectApiName}.${fieldApiName} from the app`],
    warnings: ["The physical column and every stored value will be retained"],
  };
}

function mutatePermission(
  current: RecordAppDefinition,
  payload: Record<string, unknown>,
): { definition: RecordAppDefinition; effects: string[]; warnings: string[] } {
  assertOnlyKeys(payload, ["app", "role", "object", "read", "create", "update", "delete"]);
  const role = requiredString(payload, "role");
  if (role !== "admin" && role !== "member") {
    throw new RecordSchemaActionPayloadError("role: admin or member is required");
  }
  const objectApiName = recordIdentifier(requiredString(payload, "object"));
  const canonical = recordAppDefinitionPayload(current);
  const permissions = canonical.permissions as Array<Record<string, unknown>>;
  const replacement = {
    role,
    object: objectApiName,
    read: payload.read ?? false,
    create: payload.create ?? false,
    update: payload.update ?? false,
    delete: payload.delete ?? false,
  };
  const index = permissions.findIndex(
    (permission) =>
      permission.role === role && permission.object === objectApiName,
  );
  if (index < 0) permissions.push(replacement);
  else permissions[index] = replacement;
  return {
    definition: parseCurrent(canonical),
    effects: [
      `Set ${role} CRUD grants for ${objectApiName}`,
      "Regenerate exhaustive GraphJin access policy",
    ],
    warnings: [],
  };
}

function mutateLayout(
  current: RecordAppDefinition,
  payload: Record<string, unknown>,
): { definition: RecordAppDefinition; effects: string[]; warnings: string[] } {
  const canonical = recordAppDefinitionPayload(current);
  if (payload.object !== undefined) {
    assertOnlyKeys(payload, ["app", "object", "kind", "definition"]);
    const objectApiName = recordIdentifier(requiredString(payload, "object"));
    const kind = requiredString(payload, "kind");
    if (kind !== "detail" && kind !== "list") {
      throw new RecordSchemaActionPayloadError("kind: detail or list is required");
    }
    const object = objectPayload(canonical, objectApiName);
    const layouts = object.layouts as Array<Record<string, unknown>>;
    const replacement = {
      kind,
      definition: requiredRecord(payload, "definition") as JsonObject,
    };
    const index = layouts.findIndex((layout) => layout.kind === kind);
    if (index < 0) layouts.push(replacement);
    else layouts[index] = replacement;
    return {
      definition: parseCurrent(canonical),
      effects: [`Update ${kind} layout for ${objectApiName}`],
      warnings: [],
    };
  }
  assertOnlyKeys(payload, ["app", "page", "label", "definition", "nav_order"]);
  const pageApiName = recordIdentifier(requiredString(payload, "page"));
  const pages = canonical.pages as Array<Record<string, unknown>>;
  const replacement = {
    api_name: pageApiName,
    label: payload.label ?? payload.page,
    definition: requiredRecord(payload, "definition") as JsonObject,
    nav_order: payload.nav_order ?? 0,
  };
  const index = pages.findIndex((page) => page.api_name === pageApiName);
  if (index < 0) pages.push(replacement);
  else pages[index] = replacement;
  return {
    definition: parseCurrent(canonical),
    effects: [`Update app page ${pageApiName}`],
    warnings: [],
  };
}

function applyIncrementalAction(
  action: Exclude<RecordSchemaAction, "app_create">,
  current: RecordAppDefinition,
  payload: Record<string, unknown>,
): { definition: RecordAppDefinition; effects: string[]; warnings: string[] } {
  switch (action) {
    case "app_object_create":
      return mutateObjectCreate(current, payload);
    case "app_field_add":
      return mutateFieldAdd(current, payload);
    case "app_field_modify":
      return mutateFieldModify(current, payload);
    case "app_object_archive":
      return mutateObjectArchive(current, payload);
    case "app_field_archive":
      return mutateFieldArchive(current, payload);
    case "app_permission_set":
      return mutatePermission(current, payload);
    case "app_layout_update":
      return mutateLayout(current, payload);
  }
}

function actionKind(kind: string): RecordSchemaAction {
  if (!RECORD_SCHEMA_ACTIONS.includes(kind as RecordSchemaAction)) {
    throw new RecordSchemaActionPayloadError(`unsupported records schema action: ${kind}`);
  }
  return kind as RecordSchemaAction;
}

export class RecordsSchemaPlanner {
  private readonly registry: RecordRegistry;

  constructor(
    private readonly dependencies: {
      pool: Pool;
      saga: RecordSchemaPlanStore;
      registry?: RecordRegistry;
    },
  ) {
    this.registry = dependencies.registry ?? new RecordRegistry(dependencies.pool);
  }

  private async nextRevision(orgId: string): Promise<string> {
    const result = await this.dependencies.pool.query<{ revision: string }>(
      `select coalesce(
         (select revision + 1 from engine.registry_version where org_id = $1),
         1
       )::text as revision`,
      [orgId],
    );
    return result.rows[0]?.revision ?? "1";
  }

  private async createAppStub(
    orgId: string,
    definition: RecordAppDefinition,
  ): Promise<boolean> {
    const result = await this.dependencies.pool.query(
      `insert into engine.record_app
         (org_id, app_id, label, purpose, status, nav_order)
       values ($1, $2, $3, $4, 'draft', $5)
       on conflict (org_id, app_id) do nothing
       returning app_id`,
      [
        orgId,
        definition.appId,
        definition.label,
        definition.purpose,
        definition.navOrder,
      ],
    );
    if (result.rowCount === 0) {
      throw new RecordSchemaActionPayloadError(
        `record app already exists: ${definition.appId}`,
      );
    }
    return true;
  }

  private async removeUnusedStub(orgId: string, appId: string): Promise<void> {
    await this.dependencies.pool.query(
      `delete from engine.record_app a
       where a.org_id = $1 and a.app_id = $2 and a.status = 'draft'
         and not exists (
           select 1 from engine.app_schema_change c
           where c.org_id = a.org_id and c.app_id = a.app_id
         )`,
      [orgId, appId],
    );
  }

  async prepare(request: RecordSchemaActionRequest): Promise<PreparedRecordSchemaAction> {
    if (request.actorRole !== "admin") {
      throw new RecordSchemaActionPayloadError(
        "records schema actions require an admin actor snapshot",
      );
    }
    const action = actionKind(request.kind);
    let definition: RecordAppDefinition;
    let effects: string[];
    let warnings: string[];
    let createdStub = false;

    if (action === "app_create") {
      definition = parseRecordAppDefinition(request.payload);
      effects = [
        `Create app ${definition.appId}`,
        `Create ${definition.objects.length} objects and their additive tables`,
        "Install audit triggers and exhaustive GraphJin access policy",
      ];
      warnings = [];
      createdStub = await this.createAppStub(request.orgId, definition);
    } else {
      const appId = recordIdentifier(requiredString(request.payload, "app"));
      const snapshot = await this.registry.loadApp(request.orgId, appId);
      if (!snapshot || !["active", "degraded"].includes(snapshot.app.status)) {
        throw new RecordSchemaActionPayloadError(
          `record app is missing or unavailable: ${appId}`,
        );
      }
      const mutation = applyIncrementalAction(
        action,
        recordAppDefinitionFromSnapshot(snapshot),
        request.payload,
      );
      definition = mutation.definition;
      effects = mutation.effects;
      warnings = mutation.warnings;
    }

    try {
      const desiredRevision = await this.nextRevision(request.orgId);
      const preview = previewFor(
        action,
        definition,
        desiredRevision,
        effects,
        warnings,
      );
      const canonicalDefinition = recordAppDefinitionPayload(definition);
      const change = await this.dependencies.saga.plan({
        orgId: request.orgId,
        appId: definition.appId,
        actionRequestId: request.id,
        desiredRevision,
        action,
        actorUserId: request.actorUserId,
        detail: {
          definition: canonicalDefinition,
          preview,
          payload: request.payload,
        },
        objects: await ddlMatchingPhysicalNullability(
          this.dependencies.pool,
          definition,
        ),
      });
      return {
        change,
        preview,
        payload: {
          ...request.payload,
          preview_hash: change.previewHash,
          schema_preview: preview,
        },
      };
    } catch (error) {
      if (createdStub) {
        await this.removeUnusedStub(request.orgId, definition.appId).catch(
          () => undefined,
        );
      }
      throw error;
    }
  }
}
