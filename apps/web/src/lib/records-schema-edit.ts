import "server-only";

import type { JsonObject, RecordFieldKind } from "@neko/records";
import { inProcessControlPlane } from "@neko/llm/work";
import { approveActionRequest } from "@neko/llm/workflows";
import { getCurrentActor } from "@/lib/actor";
import {
  loadRecordAppShellRaw,
  RecordAdminPermissionError,
  RecordAppRouteError,
} from "@/lib/records";
import { RecordFormExecutionError, RecordFormPolicyError } from "@/lib/records-actions";

export const SCHEMA_EDIT_KINDS = [
  "app_field_add",
  "app_field_modify",
  "app_field_archive",
  "app_layout_update",
] as const;

export type SchemaEditKind = (typeof SCHEMA_EDIT_KINDS)[number];

export type SchemaEditorField = {
  apiName: string;
  label: string;
  kind: RecordFieldKind;
  required: boolean;
  readOnly: boolean;
  picklistValues: string[];
  referenceTargets: string[];
};

export type SchemaEditorSection = { label: string; fields: string[] };

export type SchemaEditorObject = {
  apiName: string;
  label: string;
  pluralLabel: string;
  nameField: string;
  fields: SchemaEditorField[];
  listColumns: string[];
  detailSections: SchemaEditorSection[];
};

export type SchemaEditorModel = {
  revision: string;
  appId: string;
  appLabel: string;
  objects: SchemaEditorObject[];
};

function picklistStrings(values: unknown[] | null): string[] {
  return (values ?? []).flatMap((entry) => {
    if (typeof entry === "string") return [entry];
    if (entry && typeof entry === "object" && !Array.isArray(entry)) {
      const value = (entry as Record<string, unknown>).value;
      return typeof value === "string" ? [value] : [];
    }
    return [];
  });
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value)
    ? value.flatMap((entry) => {
        if (typeof entry === "string") return [entry];
        if (entry && typeof entry === "object" && !Array.isArray(entry)) {
          const field = (entry as Record<string, unknown>).field;
          return typeof field === "string" ? [field] : [];
        }
        return [];
      })
    : [];
}

function listColumns(definition: JsonObject | undefined, fields: SchemaEditorField[]): string[] {
  const live = new Set(fields.map((field) => field.apiName));
  const columns = stringArray(definition?.columns).filter((name) => live.has(name));
  return columns.length > 0 ? columns : fields.slice(0, 6).map((field) => field.apiName);
}

function detailSections(definition: JsonObject | undefined, fields: SchemaEditorField[]): SchemaEditorSection[] {
  const live = new Set(fields.map((field) => field.apiName));
  const used = new Set<string>();
  const sections = (Array.isArray(definition?.sections) ? definition.sections : []).flatMap(
    (raw, index): SchemaEditorSection[] => {
      if (!raw || typeof raw !== "object" || Array.isArray(raw)) return [];
      const section = raw as Record<string, unknown>;
      const names = stringArray(section.fields).filter((name) => live.has(name) && !used.has(name));
      names.forEach((name) => used.add(name));
      const label = typeof section.label === "string" && section.label.trim() ? section.label : `Details ${index + 1}`;
      return [{ label, fields: names }];
    },
  );
  if (sections.length === 0) return [{ label: "Details", fields: fields.map((field) => field.apiName) }];
  return sections;
}

export async function getRecordSchemaEditorModel(input: {
  orgId: string;
  appId: string;
}): Promise<SchemaEditorModel> {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") throw new RecordAdminPermissionError();
  const shell = await loadRecordAppShellRaw(input.orgId, input.appId);
  if (shell.availability !== "active") {
    throw new RecordAppRouteError(shell.degradedReason ?? "record app is degraded", 503);
  }
  const { snapshot } = shell;
  const objects = snapshot.objects
    .filter((object) => object.archivedAt === null)
    .map((object): SchemaEditorObject => {
      const fields = snapshot.fields
        .filter((field) => field.objectId === object.id && field.archivedAt === null && field.kind !== "id")
        .map((field) => ({
          apiName: String(field.apiName),
          label: field.label,
          kind: field.kind,
          required: field.required,
          readOnly: field.readOnly,
          picklistValues: picklistStrings(field.picklistValues),
          referenceTargets: field.referenceTargets ?? [],
        }));
      const layouts = snapshot.layouts.filter((layout) => layout.objectId === object.id);
      const listDefinition = layouts.find((layout) => layout.kind === "list")?.definition;
      const detailDefinition = layouts.find((layout) => layout.kind === "detail")?.definition;
      const order = new Map(
        [...stringArray(listDefinition?.columns), ...detailSections(detailDefinition, fields).flatMap((section) => section.fields)]
          .map((name, index) => [name, index] as const)
          .reverse(),
      );
      fields.sort(
        (a, b) => (order.get(a.apiName) ?? Number.MAX_SAFE_INTEGER) - (order.get(b.apiName) ?? Number.MAX_SAFE_INTEGER),
      );
      return {
        apiName: String(object.apiName),
        label: object.label,
        pluralLabel: object.pluralLabel,
        nameField: String(object.nameField),
        fields,
        listColumns: listColumns(listDefinition, fields),
        detailSections: detailSections(detailDefinition, fields),
      };
    });
  return { revision: snapshot.revision, appId: snapshot.app.appId, appLabel: snapshot.app.label, objects };
}

export class RecordSchemaChangeRejectedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "RecordSchemaChangeRejectedError";
  }
}

function rejection(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  return message.replace(/^action preflight failed:\s*/i, "");
}

const SUMMARIES: Record<SchemaEditKind, string> = {
  app_field_add: "Add a field",
  app_field_modify: "Change a field",
  app_field_archive: "Archive a field",
  app_layout_update: "Change a layout",
};

export async function submitRecordSchemaChange(input: {
  orgId: string;
  appId: string;
  kind: SchemaEditKind;
  payload: Record<string, unknown>;
}): Promise<{ status: "executed" | "queued"; actionRequestId: string }> {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") throw new RecordAdminPermissionError();
  const payload: Record<string, unknown> = { ...input.payload, app: input.appId };
  const object = typeof payload.object === "string" ? payload.object : "";
  const target = `app:${input.appId}`;
  const decision = await inProcessControlPlane.evaluateActionPolicy({
    orgId: input.orgId,
    scope: "internal",
    kind: input.kind,
    target,
    riskLevel: "medium",
  });
  if (decision.decision === "no_policy") {
    throw new RecordFormPolicyError("No action policy covers app changes. Define one in Rules first.");
  }
  if (decision.decision === "deny") throw new RecordFormPolicyError(decision.reason);
  const field = typeof payload.field === "string" ? ` ${payload.field}` : "";
  const summary = `${SUMMARIES[input.kind]}${field} on ${input.appId}${object ? `.${object}` : ""}`;
  let request: { id: string };
  try {
    request = await inProcessControlPlane.createActionRequest({
      orgId: input.orgId,
      scope: "internal",
      kind: input.kind,
      target,
      payload,
      riskLevel: "medium",
      status: decision.mode === "draft_only" ? "draft" : "pending_approval",
      policyId: decision.policy.id,
      summary,
      intent: `${summary}. The signed-in administrator submitted this change from the app editor.`,
      actorUserId: actor.userId,
      actorRole: "admin",
      actorBackend: "web-form",
    });
  } catch (error) {
    throw new RecordSchemaChangeRejectedError(rejection(error));
  }
  await approveActionRequest({
    id: request.id,
    orgId: input.orgId,
    approverUserId: actor.userId,
    approver: { userId: actor.userId, role: "admin" },
  });
  await inProcessControlPlane.enqueueActionExecute({ orgId: input.orgId, actionRequestId: request.id });
  const execution = await inProcessControlPlane.waitForActionExecution({
    orgId: input.orgId,
    actionRequestId: request.id,
    timeoutMs: 60_000,
  });
  if (execution.status === "failed") throw new RecordFormExecutionError(execution.error, request.id);
  if (input.kind === "app_field_add" && execution.status !== "timeout") {
    await placeNewFieldOnRecordPage({ orgId: input.orgId, appId: input.appId, object, payload });
  }
  return { status: execution.status === "timeout" ? "queued" : "executed", actionRequestId: request.id };
}

/** The record page shows only fields in its layout, so add the new field to the last section. */
async function placeNewFieldOnRecordPage(input: {
  orgId: string;
  appId: string;
  object: string;
  payload: Record<string, unknown>;
}): Promise<void> {
  const field = input.payload.field as Record<string, unknown> | undefined;
  const fieldName = typeof field?.api_name === "string" ? field.api_name : null;
  if (!fieldName) return;
  const shell = await loadRecordAppShellRaw(input.orgId, input.appId);
  const object = shell.snapshot.objects.find((candidate) => candidate.apiName === input.object);
  const layout = shell.snapshot.layouts.find((candidate) => candidate.objectId === object?.id && candidate.kind === "detail");
  const sections = Array.isArray(layout?.definition.sections) ? layout.definition.sections : null;
  if (!object || !sections || sections.length === 0) return;
  if (JSON.stringify(sections).includes(`"${fieldName}"`)) return;
  const next = sections.map((section, index) => {
    if (index !== sections.length - 1 || !section || typeof section !== "object" || Array.isArray(section)) return section;
    const current = section as Record<string, unknown>;
    return { ...current, fields: [...(Array.isArray(current.fields) ? current.fields : []), fieldName] };
  });
  await submitRecordSchemaChange({
    orgId: input.orgId,
    appId: input.appId,
    kind: "app_layout_update",
    payload: { object: input.object, kind: "detail", definition: { ...layout!.definition, sections: next } },
  });
}
