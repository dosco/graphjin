import type { Pool } from "pg";
import {
  mintRecordsGraphjinToken,
  recordsGraphjinSigningSecret,
  type RecordsGraphjinTransport,
} from "../graphjin/client";
import { validateRecordIdentifier } from "../naming";
import { syncRecordsActor } from "../policy/actor";
import { authorizeRecordSnapshot } from "../policy/access";
import {
  evaluateRecordObjectPermission,
  selectRecordPolicyGrant,
} from "../policy/evaluate";
import {
  buildRecordDetailQuery,
  buildRecordListQuery,
  buildRecordRecycleDetailQuery,
  buildRecordRecycleListQuery,
  projectRecordRows,
  type RecordListFilter,
  type RecordObjectView,
  type RecordViewerRole,
} from "./query";
import type { AppRegistrySnapshot, RecordApp } from "../types";

export interface RecordRegistryReader {
  listApps(orgId: string): Promise<RecordApp[]>;
  loadApp(orgId: string, appId: string): Promise<AppRegistrySnapshot | null>;
}

export type RecordsViewer = {
  orgId: string;
  userId: string;
  role: RecordViewerRole;
  groupIds?: readonly string[];
  solo?: boolean;
};

export type RecordsCatalogField = {
  apiName: string;
  sourceApiName: string | null;
  label: string;
  kind: string;
  required: boolean;
  readOnly: boolean;
  picklistValues: unknown[] | null;
  referenceTargets: string[] | null;
};

export type RecordsCatalogObject = {
  apiName: string;
  sourceApiName: string | null;
  label: string;
  pluralLabel: string;
  nameField: string;
  visibility: "org" | "owner";
  recordCount: string | null;
  permissions: {
    canRead: boolean;
    canCreate: boolean;
    canUpdate: boolean;
    canDelete: boolean;
  };
  fields: RecordsCatalogField[];
  layouts: Array<{ kind: "list" | "detail"; definition: Record<string, unknown> }>;
};

export type RecordsCatalogApp = {
  appId: string;
  label: string;
  purpose: string | null;
  revision: string;
  objects: RecordsCatalogObject[];
  pages: Array<{
    apiName: string;
    label: string;
    definition: Record<string, unknown>;
  }>;
};

export type RecordsCatalogResult = {
  apps: RecordsCatalogApp[];
};

export type RecordsQueryResult = {
  app: { appId: string; label: string };
  object: { apiName: string; label: string; pluralLabel: string };
  columns: RecordObjectView["columns"];
  rows: Array<Record<string, unknown>>;
  total: number;
  cursor: string | null;
};

export type RecordsDetailResult = Omit<RecordsQueryResult, "rows" | "total" | "cursor"> & {
  row: Record<string, unknown> | null;
};

export type RecycledRecordSummary = {
  appId: string;
  objectApiName: string;
  recordId: string;
  recordName: string;
  ownerUserId: string | null;
  deletedAt: string;
  deletedBy: string | null;
};

export type RecordsRecycleQueryResult = {
  app: { appId: string; label: string };
  object: { apiName: string; label: string; pluralLabel: string };
  rows: RecycledRecordSummary[];
  total: number;
  cursor: string | null;
};

export type RecordsRecycleDetailResult = Omit<
  RecordsRecycleQueryResult,
  "rows" | "total" | "cursor"
> & {
  row: RecycledRecordSummary | null;
};

function rowsFrom(value: unknown): Array<Record<string, unknown>> {
  return Array.isArray(value)
    ? value.filter(
        (row): row is Record<string, unknown> =>
          typeof row === "object" && row !== null && !Array.isArray(row),
      )
    : [];
}

function totalFrom(value: unknown): number {
  const summary = rowsFrom(value)[0];
  const raw = summary?.count_id ?? summary?.count;
  const total = typeof raw === "number" ? raw : Number.parseInt(String(raw ?? "0"), 10);
  return Number.isSafeInteger(total) && total >= 0 ? total : 0;
}

function recycledRowsFrom(value: unknown): RecycledRecordSummary[] {
  return rowsFrom(value).flatMap((row): RecycledRecordSummary[] => {
    if (
      typeof row.app_id !== "string" ||
      typeof row.object_api_name !== "string" ||
      typeof row.record_id !== "string" ||
      typeof row.record_name !== "string" ||
      typeof row.deleted_at !== "string"
    ) {
      return [];
    }
    return [
      {
        appId: row.app_id,
        objectApiName: row.object_api_name,
        recordId: row.record_id,
        recordName: row.record_name,
        ownerUserId: typeof row.owner_user_id === "string" ? row.owner_user_id : null,
        deletedAt: row.deleted_at,
        deletedBy: typeof row.deleted_by === "string" ? row.deleted_by : null,
      },
    ];
  });
}

function allowedApp(
  appId: string,
  activeAppIds: ReadonlySet<string> | undefined,
): boolean {
  return activeAppIds === undefined || activeAppIds.has(appId);
}

/**
 * Actor-scoped read gateway shared by trusted hosts. It only reads app data
 * through generated GraphJin documents and a short-lived human JWT; the pool
 * is used solely for the engine registry and actor-policy projection.
 */
export class RecordsReadGateway {
  constructor(
    private readonly pool: Pool,
    private readonly registry: RecordRegistryReader,
    private readonly graphjin: RecordsGraphjinTransport,
  ) {}

  async listCatalog(input: {
    viewer: RecordsViewer;
    appId?: string;
    activeAppIds?: ReadonlySet<string>;
  }): Promise<RecordsCatalogResult> {
    const requestedAppId = input.appId
      ? validateRecordIdentifier(input.appId)
      : null;
    const apps = requestedAppId
      ? [await this.registry.loadApp(input.viewer.orgId, requestedAppId)]
      : await Promise.all(
          (await this.registry.listApps(input.viewer.orgId)).map((app) =>
            this.registry.loadApp(input.viewer.orgId, app.appId),
          ),
        );

    return {
      apps: (await Promise.all(apps.map(async (snapshot) =>
        snapshot ? authorizeRecordSnapshot(this.pool, snapshot, input.viewer) : null,
      ))).flatMap((snapshot): RecordsCatalogApp[] => {
        if (
          !snapshot ||
          snapshot.app.status !== "active" ||
          !allowedApp(snapshot.app.appId, input.activeAppIds)
        ) {
          return [];
        }
        const objects = snapshot.objects.flatMap((object): RecordsCatalogObject[] => {
          if (object.archivedAt !== null) return [];
          const permission = selectRecordPolicyGrant(snapshot.permissions, {
            appId: snapshot.app.appId,
            objectApiName: object.apiName,
            role: input.viewer.role,
          });
          if (
            !permission ||
            !evaluateRecordObjectPermission({
              actor: { role: input.viewer.role },
              object: {
                appId: object.appId,
                apiName: object.apiName,
                visibility: object.visibility,
                archived: object.archivedAt !== null,
              },
              grant: permission,
              operation: "read",
            })
          ) {
            return [];
          }
          return [
            {
              apiName: object.apiName,
              sourceApiName: object.sourceApiName,
              label: object.label,
              pluralLabel: object.pluralLabel,
              nameField: object.nameField,
              visibility: object.visibility,
              recordCount: object.recordCount,
              permissions: {
                canRead: permission.canRead,
                canCreate: permission.canCreate,
                canUpdate: permission.canUpdate,
                canDelete: permission.canDelete,
              },
              fields: snapshot.fields
                .filter(
                  (field) => field.objectId === object.id && field.archivedAt === null,
                )
                .map((field) => ({
                  apiName: field.apiName,
                  sourceApiName: field.sourceApiName,
                  label: field.label,
                  kind: field.kind,
                  required: field.required,
                  readOnly: field.readOnly,
                  picklistValues: field.picklistValues,
                  referenceTargets: field.referenceTargets,
                })),
              layouts: snapshot.layouts
                .filter((layout) => layout.objectId === object.id)
                .map((layout) => ({
                  kind: layout.kind,
                  definition: layout.definition,
                })),
            },
          ];
        });
        return objects.length === 0
          ? []
          : [
              {
                appId: snapshot.app.appId,
                label: snapshot.app.label,
                purpose: snapshot.app.purpose,
                revision: snapshot.revision,
                objects,
                pages: snapshot.pages.map((page) => ({
                  apiName: page.apiName,
                  label: page.label,
                  definition: page.definition,
                })),
              },
            ];
      }),
    };
  }

  private async activeSnapshot(input: {
    viewer: RecordsViewer;
    appId: string;
    activeAppIds?: ReadonlySet<string>;
  }): Promise<AppRegistrySnapshot> {
    const appId = validateRecordIdentifier(input.appId);
    if (!allowedApp(appId, input.activeAppIds)) {
      throw new Error("record app is not active");
    }
    const snapshot = await this.registry.loadApp(input.viewer.orgId, appId);
    if (!snapshot || snapshot.app.status !== "active") {
      throw new Error("record app is not active");
    }
    const authorized = await authorizeRecordSnapshot(
      this.pool,
      snapshot,
      input.viewer,
    );
    if (!authorized) throw new Error("record app is not active or accessible");
    return authorized;
  }

  private async token(viewer: RecordsViewer): Promise<string> {
    await syncRecordsActor(this.pool, viewer);
    return mintRecordsGraphjinToken({
      secret: recordsGraphjinSigningSecret(viewer.orgId),
      orgId: viewer.orgId,
      userId: viewer.userId,
      role: viewer.role,
    });
  }

  async findRecords(input: {
    viewer: RecordsViewer;
    activeAppIds?: ReadonlySet<string>;
    appId: string;
    objectApiName: string;
    first?: number;
    after?: string | null;
    sort?: { field: string; direction: "asc" | "desc" };
    search?: string | null;
    filters?: RecordListFilter[];
    myRecords?: boolean;
  }): Promise<RecordsQueryResult> {
    const snapshot = await this.activeSnapshot(input);
    const generated = buildRecordListQuery({
      snapshot,
      objectApiName: input.objectApiName,
      role: input.viewer.role,
      userId: input.viewer.userId,
      first: input.first,
      after: input.after,
      sort: input.sort,
      search: input.search,
      filters: input.filters,
      myRecords: input.myRecords,
    });
    const data = await this.graphjin.execute<Record<string, unknown>>({
      operationName: generated.operationName,
      query: generated.query,
      variables: generated.variables,
      token: await this.token(input.viewer),
    });
    return {
      app: { appId: snapshot.app.appId, label: snapshot.app.label },
      object: {
        apiName: generated.view.object.apiName,
        label: generated.view.object.label,
        pluralLabel: generated.view.object.pluralLabel,
      },
      columns: generated.view.columns,
      rows: projectRecordRows(generated.view, rowsFrom(data[generated.resultField])),
      total: totalFrom(data[generated.totalField]),
      cursor:
        typeof data[generated.cursorField] === "string"
          ? String(data[generated.cursorField])
          : null,
    };
  }

  async getRecord(input: {
    viewer: RecordsViewer;
    activeAppIds?: ReadonlySet<string>;
    appId: string;
    objectApiName: string;
    recordId: string;
    allFields?: boolean;
  }): Promise<RecordsDetailResult> {
    const snapshot = await this.activeSnapshot(input);
    const generated = buildRecordDetailQuery({
      snapshot,
      objectApiName: input.objectApiName,
      role: input.viewer.role,
      recordId: input.recordId,
      allFields: input.allFields,
    });
    const data = await this.graphjin.execute<Record<string, unknown>>({
      operationName: generated.operationName,
      query: generated.query,
      variables: generated.variables,
      token: await this.token(input.viewer),
    });
    return {
      app: { appId: snapshot.app.appId, label: snapshot.app.label },
      object: {
        apiName: generated.view.object.apiName,
        label: generated.view.object.label,
        pluralLabel: generated.view.object.pluralLabel,
      },
      columns: generated.view.columns,
      row: projectRecordRows(generated.view, rowsFrom(data[generated.resultField]))[0] ?? null,
    };
  }

  async findRecycledRecords(input: {
    viewer: RecordsViewer;
    activeAppIds?: ReadonlySet<string>;
    appId: string;
    objectApiName: string;
    first?: number;
    after?: string | null;
    search?: string | null;
  }): Promise<RecordsRecycleQueryResult> {
    const snapshot = await this.activeSnapshot(input);
    const generated = buildRecordRecycleListQuery({
      snapshot,
      objectApiName: input.objectApiName,
      role: input.viewer.role,
      first: input.first,
      after: input.after,
      search: input.search,
    });
    const data = await this.graphjin.execute<Record<string, unknown>>({
      operationName: generated.operationName,
      query: generated.query,
      variables: generated.variables,
      token: await this.token(input.viewer),
    });
    return {
      app: { appId: snapshot.app.appId, label: snapshot.app.label },
      object: {
        apiName: generated.view.object.apiName,
        label: generated.view.object.label,
        pluralLabel: generated.view.object.pluralLabel,
      },
      rows: recycledRowsFrom(data[generated.resultField]),
      total: totalFrom(data[generated.totalField]),
      cursor:
        typeof data[generated.cursorField] === "string"
          ? String(data[generated.cursorField])
          : null,
    };
  }

  async getRecycledRecord(input: {
    viewer: RecordsViewer;
    activeAppIds?: ReadonlySet<string>;
    appId: string;
    objectApiName: string;
    recordId: string;
  }): Promise<RecordsRecycleDetailResult> {
    const snapshot = await this.activeSnapshot(input);
    const generated = buildRecordRecycleDetailQuery({
      snapshot,
      objectApiName: input.objectApiName,
      role: input.viewer.role,
      recordId: input.recordId,
    });
    const data = await this.graphjin.execute<Record<string, unknown>>({
      operationName: generated.operationName,
      query: generated.query,
      variables: generated.variables,
      token: await this.token(input.viewer),
    });
    return {
      app: { appId: snapshot.app.appId, label: snapshot.app.label },
      object: {
        apiName: generated.view.object.apiName,
        label: generated.view.object.label,
        pluralLabel: generated.view.object.pluralLabel,
      },
      row: recycledRowsFrom(data[generated.resultField])[0] ?? null,
    };
  }
}
