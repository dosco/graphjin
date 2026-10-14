import "server-only";

import pg from "pg";
import {
  action_request,
  and,
  app_user,
  app_state,
  buildRecordsPoolConfig,
  db,
  desc,
  eq,
  inArray,
  resolveUserGroups,
} from "@neko/db";
import {
  authorizeRecordSnapshot,
  buildRecordDetailQuery,
  buildRecordChangeLogQuery,
  buildRecordAggregateQuery,
  buildRecordListQuery,
  buildRecordRecycleDetailQuery,
  buildRecordRecycleListQuery,
  buildRecordReferenceLabelQuery,
  projectRecordRows,
  buildRecordSchemaLogQuery,
  evaluateRecordObjectPermission,
  mintRecordsGraphjinToken,
  parseRecordAppPage,
  RecordRegistry,
  RecordSavedViewStore,
  recordsGraphjinSigningSecret,
  RecordsGraphjinClient,
  syncRecordsActor,
  selectRecordPolicyGrant,
  validateRecordIdentifier,
  type AppRegistrySnapshot,
  type JsonObject,
  type RecordListFilter,
  type RecordFilterExpression,
  type RecordObjectView,
  type RecordViewColumn,
  type RecordSavedView,
  type RecycledRecordSummary,
  type RecordViewerRole,
} from "@neko/records";
import { getCurrentActor } from "@/lib/actor";
import {
  recordReferenceIdentityKey,
  type RecordReferenceIdentity,
} from "@/lib/records-reference";
import {
  groupPendingRecordActions,
  PENDING_RECORD_ACTION_STATUSES,
  type PendingRecordAction,
} from "@/lib/records-pending";

const RECORDS_GRAPHJIN_URL =
  process.env.OPENNEKO_RECORDS_GRAPHJIN_URL ?? "http://127.0.0.1:8090";

type RecordsRuntime = {
  pool: pg.Pool;
  registry: RecordRegistry;
  savedViews: RecordSavedViewStore;
  graphjin: RecordsGraphjinClient;
};

const runtimeGlobal = globalThis as typeof globalThis & {
  __opennekoWebRecordsRuntime?: RecordsRuntime;
};

function createRuntime(): RecordsRuntime {
  const pool = new pg.Pool({
    ...buildRecordsPoolConfig(),
    application_name: "openneko-web-records",
  });
  const registry = new RecordRegistry(pool);
  return {
    pool,
    registry,
    savedViews: new RecordSavedViewStore(pool, registry),
    graphjin: new RecordsGraphjinClient({ baseUrl: RECORDS_GRAPHJIN_URL }),
  };
}

function recordsRuntime(): RecordsRuntime {
  const existing = runtimeGlobal.__opennekoWebRecordsRuntime;
  if (existing) return existing;
  const created = createRuntime();
  runtimeGlobal.__opennekoWebRecordsRuntime = created;
  return created;
}

/** Server-only access to records operational state (never app-table data). */
export function getWebRecordsPool(): pg.Pool {
  return recordsRuntime().pool;
}

/** Drain records connections so rotated credentials are picked up immediately. */
export async function reconnectWebRecordsRuntime(): Promise<void> {
  const current = runtimeGlobal.__opennekoWebRecordsRuntime;
  runtimeGlobal.__opennekoWebRecordsRuntime = undefined;
  if (current) await current.pool.end();
}

export const resetWebRecordsRuntimeForTesting = reconnectWebRecordsRuntime;

export type RecordAppNavObject = {
  apiName: string;
  label: string;
  pluralLabel: string;
  recordCount: string | null;
  custom: boolean;
  canCreate: boolean;
};

export type RecordAppNavItem = {
  appId: string;
  label: string;
  purpose: string | null;
  objects: RecordAppNavObject[];
};

export type RecordAppShell = {
  snapshot: AppRegistrySnapshot;
  metadataStatus: string;
  availability: "active" | "degraded";
  degradedReason: string | null;
  config: JsonObject;
};

export class RecordAppRouteError extends Error {
  readonly code = "records_app_route_unavailable";

  constructor(
    message: string,
    readonly status: 404 | 409 | 503,
  ) {
    super(message);
    this.name = "RecordAppRouteError";
  }
}

export class RecordAdminPermissionError extends Error {
  readonly code = "records_admin_permission_denied";

  constructor() {
    super("Record app administration requires an admin.");
    this.name = "RecordAdminPermissionError";
  }
}

function viewerPermission(
  snapshot: AppRegistrySnapshot,
  role: RecordViewerRole,
  objectApiName: string,
  operation: "read" | "create" | "update" | "delete" = "read",
) {
  const object = snapshot.objects.find(
    (candidate) => candidate.apiName === objectApiName,
  );
  if (!object) return null;
  const permission = selectRecordPolicyGrant(snapshot.permissions, {
    appId: snapshot.app.appId,
    role,
    objectApiName: object.apiName,
  });
  return evaluateRecordObjectPermission({
    actor: { role },
    object: {
      appId: object.appId,
      apiName: object.apiName,
      visibility: object.visibility,
      archived: object.archivedAt !== null,
    },
    grant: permission,
    operation,
  })
    ? permission
    : null;
}

async function metadataAppState(orgId: string, appId: string): Promise<{
  status: string;
  config: JsonObject;
} | null> {
  const rows = await db()
    .select({ status: app_state.status, config: app_state.config })
    .from(app_state)
    .where(and(eq(app_state.org_id, orgId), eq(app_state.app_id, appId)))
    .limit(1);
  const row = rows[0];
  return row ? { status: row.status, config: row.config } : null;
}

function canonicalAppId(value: string): string {
  try {
    return validateRecordIdentifier(value);
  } catch {
    throw new RecordAppRouteError("record app was not found", 404);
  }
}

/** The full app definition, unfiltered by the viewer's grants. Use only behind an admin check. */
export async function loadRecordAppShellRaw(
  orgId: string,
  appId: string,
): Promise<RecordAppShell> {
  const canonical = canonicalAppId(appId);
  const [state, snapshot] = await Promise.all([
    metadataAppState(orgId, canonical),
    recordsRuntime().registry.loadApp(orgId, canonical),
  ]);
  if (!state || !snapshot || snapshot.app.status === "archived") {
    throw new RecordAppRouteError("record app was not found", 404);
  }
  const active = state.status === "active" && snapshot.app.status === "active";
  const degraded = state.status === "degraded" || snapshot.app.status === "degraded";
  if (!active && !degraded) {
    throw new RecordAppRouteError("record app is not available yet", 409);
  }
  return {
    snapshot,
    metadataStatus: state.status,
    availability: active ? "active" : "degraded",
    degradedReason: active
      ? null
      : state.status !== snapshot.app.status
        ? `Metadata reports ${state.status}; the records registry reports ${snapshot.app.status}.`
        : "The records data plane is degraded and reads are paused.",
    config: state.config,
  };
}

export async function loadRecordAppShell(
  orgId: string,
  appId: string,
): Promise<RecordAppShell> {
  const [shell, viewer] = await Promise.all([
    loadRecordAppShellRaw(orgId, appId),
    recordsViewer(orgId),
  ]);
  const snapshot = await authorizeRecordSnapshot(
    recordsRuntime().pool,
    shell.snapshot,
    viewer,
  );
  if (!snapshot) throw new RecordAppRouteError("record app was not found", 404);
  return { ...shell, snapshot };
}

export async function listRecordAppsForViewer(input: {
  orgId: string;
  role: RecordViewerRole;
}): Promise<RecordAppNavItem[]> {
  const viewer = await recordsViewer(input.orgId);
  const states = await db()
    .select({ appId: app_state.app_id })
    .from(app_state)
    .where(and(eq(app_state.org_id, input.orgId), eq(app_state.status, "active")));
  const activeMetadata = new Set(states.map((row) => row.appId));
  const apps = (await recordsRuntime().registry.listApps(input.orgId)).filter(
    (app) => app.status === "active" && activeMetadata.has(app.appId),
  );
  const snapshots = await Promise.all(apps.map(async (app) => {
    const snapshot = await recordsRuntime().registry.loadApp(input.orgId, app.appId);
    return snapshot
      ? authorizeRecordSnapshot(recordsRuntime().pool, snapshot, viewer)
      : null;
  }));
  return snapshots.flatMap((snapshot): RecordAppNavItem[] => {
    if (!snapshot) return [];
    const objects = snapshot.objects.flatMap((object): RecordAppNavObject[] => {
      if (object.archivedAt !== null) return [];
      const permission = viewerPermission(snapshot, viewer.role, object.apiName);
      if (!permission) return [];
      return [
        {
          apiName: object.apiName,
          label: object.label,
          pluralLabel: object.pluralLabel,
          recordCount: object.recordCount,
          custom: object.custom,
          canCreate: Boolean(
            viewerPermission(snapshot, viewer.role, object.apiName, "create"),
          ),
        },
      ];
    });
    return objects.length > 0
      ? [
          {
            appId: snapshot.app.appId,
            label: snapshot.app.label,
            purpose: snapshot.app.purpose,
            objects,
          },
        ]
      : [];
  });
}

export async function getRecordAppNav(input: {
  orgId: string;
  appId: string;
  role: RecordViewerRole;
}): Promise<RecordAppNavItem> {
  const shell = await loadRecordAppShell(input.orgId, input.appId);
  const viewer = await recordsViewer(input.orgId);
  if (shell.availability !== "active") {
    throw new RecordAppRouteError(shell.degradedReason ?? "record app is degraded", 503);
  }
  const objects = shell.snapshot.objects.flatMap((object): RecordAppNavObject[] => {
    if (object.archivedAt !== null) return [];
    const permission = viewerPermission(shell.snapshot, viewer.role, object.apiName);
    if (!permission) return [];
    return [
      {
        apiName: object.apiName,
        label: object.label,
        pluralLabel: object.pluralLabel,
        recordCount: object.recordCount,
        custom: object.custom,
        canCreate: Boolean(
          viewerPermission(shell.snapshot, viewer.role, object.apiName, "create"),
        ),
      },
    ];
  });
  if (objects.length === 0) throw new RecordAppRouteError("record app was not found", 404);
  return {
    appId: shell.snapshot.app.appId,
    label: shell.snapshot.app.label,
    purpose: shell.snapshot.app.purpose,
    objects,
  };
}

export type RecordSchemaHistoryEntry = {
  id: string;
  action: string;
  detail: JsonObject;
  actorUserId: string | null;
  actionRequestId: string | null;
  at: string;
};

export async function getRecordAdminModel(input: {
  orgId: string;
  appId: string;
}): Promise<{
  app: AppRegistrySnapshot["app"];
  objects: AppRegistrySnapshot["objects"];
  permissions: AppRegistrySnapshot["permissions"];
  history: RecordSchemaHistoryEntry[];
  fallbackHref: string;
}> {
  const shell = await loadRecordAppShellRaw(input.orgId, input.appId);
  if (shell.availability !== "active") {
    throw new RecordAppRouteError(shell.degradedReason ?? "record app is degraded", 503);
  }
  const viewer = await recordsViewer(input.orgId);
  if (viewer.role !== "admin") throw new RecordAdminPermissionError();
  const generated = buildRecordSchemaLogQuery({
    snapshot: shell.snapshot,
    role: viewer.role,
    first: 100,
  });
  const data = await recordsRuntime().graphjin.execute<Record<string, unknown>>({
    operationName: generated.operationName,
    query: generated.query,
    variables: generated.variables,
    token: viewer.token,
  });
  const objects = shell.snapshot.objects.filter((object) => object.archivedAt === null);
  return {
    app: shell.snapshot.app,
    objects,
    permissions: shell.snapshot.permissions,
    history: rowsFrom(data[generated.resultField]).flatMap((row): RecordSchemaHistoryEntry[] => {
      if (
        (typeof row.id !== "string" && typeof row.id !== "number") ||
        typeof row.action !== "string" ||
        typeof row.at !== "string" ||
        !row.detail ||
        typeof row.detail !== "object" ||
        Array.isArray(row.detail)
      ) return [];
      return [{
        id: String(row.id),
        action: row.action,
        detail: row.detail as JsonObject,
        actorUserId: typeof row.actor_user_id === "string" ? row.actor_user_id : null,
        actionRequestId:
          typeof row.action_request_id === "string" ? row.action_request_id : null,
        at: row.at,
      }];
    }),
    fallbackHref: objects[0]
      ? `/a/${shell.snapshot.app.appId}/${objects[0].apiName}`
      : `/a/${shell.snapshot.app.appId}`,
  };
}

async function recordsViewer(orgId: string): Promise<{
  role: RecordViewerRole;
  userId: string;
  groupIds: string[];
  solo: boolean;
  token: string;
}> {
  const actor = await getCurrentActor();
  const role: RecordViewerRole = actor.role === "member" ? "member" : "admin";
  const userId = actor.userId ?? `urn:openneko:solo-admin:${orgId}`;
  const solo = actor.userId === null;
  const groups = solo ? null : await resolveUserGroups(orgId, userId);
  await syncRecordsActor(recordsRuntime().pool, { orgId, userId, role });
  return {
    role,
    userId,
    groupIds: groups ? [...groups.groupIds, ...groups.ssoGroupIds] : [],
    solo,
    token: mintRecordsGraphjinToken({
      secret: recordsGraphjinSigningSecret(orgId),
      orgId,
      userId,
      role,
    }),
  };
}

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

export type RecordListResult = {
  rows: Array<Record<string, unknown>>;
  cursor: string | null;
  total: number;
  view: RecordObjectView;
  app: AppRegistrySnapshot["app"];
  owners: Record<string, RecordOwnerIdentity>;
  references: Record<string, RecordReferenceIdentity>;
  pendingActions: Record<string, PendingRecordAction[]>;
};

export async function listRecordSavedViews(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
}): Promise<RecordSavedView[]> {
  const viewer = await recordsViewer(input.orgId);
  return recordsRuntime().savedViews.list({
    ...input,
    userId: viewer.userId,
    role: viewer.role,
  });
}

export async function saveRecordSavedView(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
  label: string;
  shared: boolean;
  definition: unknown;
}): Promise<RecordSavedView> {
  const viewer = await recordsViewer(input.orgId);
  return recordsRuntime().savedViews.save({
    ...input,
    userId: viewer.userId,
    role: viewer.role,
  });
}

export async function deleteRecordSavedView(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
  id: string;
}): Promise<boolean> {
  const viewer = await recordsViewer(input.orgId);
  return recordsRuntime().savedViews.remove({
    ...input,
    userId: viewer.userId,
    role: viewer.role,
  });
}

export type RecordOwnerIdentity = {
  userId: string;
  label: string;
  email: string | null;
  status: "linked" | "unlinked" | "conflict" | "ignored";
};

export type RecordAppPageMetricBlock = {
  renderer: "metric";
  label: string;
  span: 1 | 2 | 3;
  value: unknown;
  valueField: string;
  field: RecordViewColumn | null;
};

export type RecordAppPageRowsBlock = {
  renderer: "list" | "timeline";
  label: string;
  span: 1 | 2 | 3;
  rows: Array<Record<string, unknown>>;
  view: RecordObjectView;
  owners: Record<string, RecordOwnerIdentity>;
  references: Record<string, RecordReferenceIdentity>;
};

export type RecordAppPageResult = {
  app: AppRegistrySnapshot["app"];
  page: AppRegistrySnapshot["pages"][number];
  blocks: Array<RecordAppPageMetricBlock | RecordAppPageRowsBlock>;
};

export async function readRecordAppPage(input: {
  orgId: string;
  appId: string;
  pageApiName?: string;
}): Promise<RecordAppPageResult | null> {
  const shell = await loadRecordAppShell(input.orgId, input.appId);
  if (shell.availability !== "active") {
    throw new RecordAppRouteError(shell.degradedReason ?? "record app is degraded", 503);
  }
  const page = input.pageApiName
    ? shell.snapshot.pages.find((candidate) => candidate.apiName === input.pageApiName)
    : shell.snapshot.pages[0];
  if (!page) return null;
  const definition = parseRecordAppPage(page.definition);
  const viewer = await recordsViewer(input.orgId);
  const blocks = await Promise.all(
    definition.blocks.map(async (block): Promise<RecordAppPageMetricBlock | RecordAppPageRowsBlock> => {
      if (block.renderer === "metric") {
        const generated = buildRecordAggregateQuery({
          snapshot: shell.snapshot,
          objectApiName: block.query.object,
          role: viewer.role,
          aggregate: block.query.aggregate!,
          field: block.query.field,
          filter: block.query.filter,
        });
        const data = await recordsRuntime().graphjin.execute<Record<string, unknown>>({
          operationName: generated.operationName,
          query: generated.query,
          variables: generated.variables,
          token: viewer.token,
        });
        const metric = rowsFrom(data[generated.resultField])[0];
        const field = block.query.field
          ? generated.view.columns.find((column) => column.apiName === block.query.field) ?? null
          : null;
        return {
          renderer: "metric",
          label: block.label,
          span: block.span,
          value: metric?.[generated.valueField] ?? 0,
          valueField: generated.valueField,
          field,
        };
      }
      const generated = buildRecordListQuery({
        snapshot: shell.snapshot,
        objectApiName: block.query.object,
        role: viewer.role,
        userId: viewer.userId,
        first: block.query.limit,
        filter: block.query.filter,
        sort: block.query.sort ?? undefined,
      });
      const data = await recordsRuntime().graphjin.execute<Record<string, unknown>>({
        operationName: generated.operationName,
        query: generated.query,
        variables: generated.variables,
        token: viewer.token,
      });
      const rows = projectRecordRows(generated.view, rowsFrom(data[generated.resultField]));
      return {
        renderer: block.renderer,
        label: block.label,
        span: block.span,
        rows,
        view: generated.view,
        owners: await resolveOwnerIdentities({
          orgId: input.orgId,
          appId: shell.snapshot.app.appId,
          rows,
        }),
        references: await resolveReferenceIdentities({
          snapshot: shell.snapshot,
          view: generated.view,
          rows,
          role: viewer.role,
          token: viewer.token,
        }),
      };
    }),
  );
  return { app: shell.snapshot.app, page, blocks };
}

async function resolveOwnerIdentities(input: {
  orgId: string;
  appId: string;
  rows: Array<Record<string, unknown>>;
}): Promise<Record<string, RecordOwnerIdentity>> {
  const ownerIds = [
    ...new Set(
      input.rows
        .map((row) => row.owner_user_id)
        .filter((value): value is string => typeof value === "string" && Boolean(value)),
    ),
  ];
  if (ownerIds.length === 0) return {};
  const identities = await recordsRuntime().pool.query<{
    source_user_id: string;
    source_email: string;
    source_name: string | null;
    app_user_id: string | null;
    status: "linked" | "unlinked" | "conflict" | "ignored";
  }>(
    `select source_user_id, source_email, source_name, app_user_id, status
     from engine.identity_map
     where org_id = $1 and app_id = $2
       and (source_user_id = any($3::text[]) or app_user_id = any($3::text[]))`,
    [input.orgId, input.appId, ownerIds],
  );
  const metadataIds = [
    ...new Set([
      ...ownerIds,
      ...identities.rows
        .map((identity) => identity.app_user_id)
        .filter((value): value is string => value !== null),
    ]),
  ];
  const users = await db()
    .select({ id: app_user.id, name: app_user.name, email: app_user.email })
    .from(app_user)
    .where(and(eq(app_user.org_id, input.orgId), inArray(app_user.id, metadataIds)));
  const usersById = new Map(users.map((user) => [user.id, user]));
  return Object.fromEntries(
    ownerIds.map((ownerId): [string, RecordOwnerIdentity] => {
      const identity = identities.rows.find(
        (candidate) =>
          candidate.source_user_id === ownerId || candidate.app_user_id === ownerId,
      );
      const user = usersById.get(identity?.app_user_id ?? ownerId);
      return [
        ownerId,
        {
          userId: identity?.app_user_id ?? ownerId,
          label:
            user?.name ??
            identity?.source_name ??
            user?.email ??
            identity?.source_email ??
            ownerId,
          email: user?.email ?? identity?.source_email ?? null,
          status: identity?.status ?? (user ? "linked" : "unlinked"),
        },
      ];
    }),
  );
}

async function resolveReferenceIdentities(input: {
  snapshot: AppRegistrySnapshot;
  view: RecordObjectView;
  rows: Array<Record<string, unknown>>;
  role: RecordViewerRole;
  token: string;
}): Promise<Record<string, RecordReferenceIdentity>> {
  const columns = input.view.columns.filter(
    (column) => column.kind === "reference" && column.referenceTargets?.length,
  );
  if (columns.length === 0 || input.rows.length === 0) return {};

  const candidates = new Map<string, RecordReferenceIdentity[]>();
  await Promise.all(
    columns.flatMap((column) => {
      const recordIds = [
        ...new Set(
          input.rows.flatMap((row) => {
            const value = row[column.columnName];
            return typeof value === "string" && value ? [value] : [];
          }),
        ),
      ];
      if (recordIds.length === 0) return [];
      return (column.referenceTargets ?? []).map(async (target) => {
        let generated;
        try {
          generated = buildRecordReferenceLabelQuery({
            snapshot: input.snapshot,
            objectApiName: target,
            role: input.role,
            recordIds,
          });
        } catch {
          // An archived or unreadable target remains a raw, non-expanded id.
          // The source field is already readable, but the resolver never
          // widens access merely to improve presentation.
          return;
        }
        const data = await recordsRuntime().graphjin.execute<Record<string, unknown>>({
          operationName: generated.operationName,
          query: generated.query,
          variables: generated.variables,
          token: input.token,
        });
        for (const row of projectRecordRows(generated.view, rowsFrom(data[generated.resultField]))) {
          if (row.id === null || row.id === undefined) continue;
          const id = String(row.id);
          const key = recordReferenceIdentityKey(column.apiName, id);
          const identity = {
            id,
            label: String(row[generated.view.object.nameField] ?? id),
            objectApiName: generated.view.object.apiName,
          };
          const existing = candidates.get(key);
          if (existing) existing.push(identity);
          else candidates.set(key, [identity]);
        }
      });
    }),
  );

  return Object.fromEntries(
    [...candidates.entries()].flatMap(([key, matches]) =>
      matches.length === 1 ? [[key, matches[0]!]] : [],
    ),
  );
}

async function resolvePendingRecordActions(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
  recordIds: string[];
}): Promise<Record<string, PendingRecordAction[]>> {
  if (input.recordIds.length === 0) return {};
  const rows = await db()
    .select({
      id: action_request.id,
      kind: action_request.kind,
      status: action_request.status,
      summary: action_request.summary,
      payload: action_request.payload,
      createdAt: action_request.created_at,
    })
    .from(action_request)
    .where(
      and(
        eq(action_request.org_id, input.orgId),
        eq(action_request.target, `record:${input.appId}/${input.objectApiName}`),
        inArray(action_request.status, [...PENDING_RECORD_ACTION_STATUSES]),
        inArray(action_request.kind, [
          "record_update",
          "record_delete",
          "record_restore",
        ]),
      ),
    )
    .orderBy(desc(action_request.created_at))
    .limit(500);
  return groupPendingRecordActions({ ...input, rows });
}

export async function readRecordList(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
  first?: number;
  after?: string | null;
  sort?: { field: string; direction: "asc" | "desc" };
  search?: string | null;
  filters?: RecordListFilter[];
  filter?: RecordFilterExpression | null;
  myRecords?: boolean;
  columns?: string[];
  includePendingActions?: boolean;
}): Promise<RecordListResult> {
  const shell = await loadRecordAppShell(input.orgId, input.appId);
  if (shell.availability !== "active") {
    throw new RecordAppRouteError(shell.degradedReason ?? "record app is degraded", 503);
  }
  const viewer = await recordsViewer(input.orgId);
  const generated = buildRecordListQuery({
    snapshot: shell.snapshot,
    objectApiName: input.objectApiName,
    role: viewer.role,
    userId: viewer.userId,
    first: input.first,
    after: input.after,
    sort: input.sort,
    search: input.search,
    filters: input.filters,
    filter: input.filter,
    myRecords: input.myRecords,
    columns: input.columns,
  });
  const data = await recordsRuntime().graphjin.execute<Record<string, unknown>>({
    operationName: generated.operationName,
    query: generated.query,
    variables: generated.variables,
    token: viewer.token,
  });
  const rows = projectRecordRows(generated.view, rowsFrom(data[generated.resultField]));
  const [owners, references, pendingActions] = await Promise.all([
    resolveOwnerIdentities({
      orgId: input.orgId,
      appId: shell.snapshot.app.appId,
      rows,
    }),
    resolveReferenceIdentities({
      snapshot: shell.snapshot,
      view: generated.view,
      rows,
      role: viewer.role,
      token: viewer.token,
    }),
    input.includePendingActions === false
      ? Promise.resolve({} as Record<string, PendingRecordAction[]>)
      : resolvePendingRecordActions({
          orgId: input.orgId,
          appId: shell.snapshot.app.appId,
          objectApiName: generated.view.object.apiName,
          recordIds: rows.flatMap((row) =>
            row.id === null || row.id === undefined ? [] : [String(row.id)],
          ),
        }),
  ]);
  return {
    rows,
    cursor:
      typeof data[generated.cursorField] === "string"
        ? String(data[generated.cursorField])
        : null,
    total: totalFrom(data[generated.totalField]),
    view: generated.view,
    app: shell.snapshot.app,
    owners,
    references,
    pendingActions,
  };
}

export async function readRecordReferenceOptions(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
  search: string;
}): Promise<Array<{ id: string; label: string }>> {
  const query = input.search.trim();
  if (!query || query.length > 200) return [];
  const shell = await loadRecordAppShell(input.orgId, input.appId);
  const target = shell.snapshot.objects.find(
    (object) => object.apiName === input.objectApiName && object.archivedAt === null,
  );
  if (!target) throw new RecordAppRouteError("record object was not found", 404);
  const result = await readRecordList({
    orgId: input.orgId,
    appId: input.appId,
    objectApiName: input.objectApiName,
    first: 10,
    search: query,
    columns: [target.nameField],
    includePendingActions: false,
  });
  return result.rows.flatMap((row) => {
    if (row.id === null || row.id === undefined) return [];
    return [{
      id: String(row.id),
      label: String(row[result.view.object.nameField] ?? row.id),
    }];
  });
}

export type RecordDetailResult = {
  row: Record<string, unknown> | null;
  view: RecordObjectView;
  app: AppRegistrySnapshot["app"];
  owners: Record<string, RecordOwnerIdentity>;
  references: Record<string, RecordReferenceIdentity>;
  layout: JsonObject | null;
  relatedLists: RecordRelatedList[];
  history: RecordChangeLogEntry[];
  pendingActions: PendingRecordAction[];
};

export type RecordRelatedList = {
  label: string;
  fieldApiName: string;
  view: RecordObjectView;
  rows: Array<Record<string, unknown>>;
  owners: Record<string, RecordOwnerIdentity>;
  references: Record<string, RecordReferenceIdentity>;
};

export type RecordChangeLogEntry = {
  id: string;
  action: string;
  actorUserId: string | null;
  actionRequestId: string | null;
  changes: JsonObject;
  at: string;
};

/** Drop change-log entries for fields the viewer has no grant to read. */
function readableHistory(
  rows: Array<Record<string, unknown>>,
  readableKeys: ReadonlySet<string>,
): Array<Record<string, unknown>> {
  return rows.map((row) => {
    const changes = row.changes;
    if (!changes || typeof changes !== "object" || Array.isArray(changes)) return row;
    return {
      ...row,
      changes: Object.fromEntries(
        Object.entries(changes as Record<string, unknown>).filter(([key]) => readableKeys.has(key)),
      ),
    };
  });
}

export async function readRecordDetail(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
  recordId: string;
  allFields?: boolean;
}): Promise<RecordDetailResult> {
  const shell = await loadRecordAppShell(input.orgId, input.appId);
  if (shell.availability !== "active") {
    throw new RecordAppRouteError(shell.degradedReason ?? "record app is degraded", 503);
  }
  const viewer = await recordsViewer(input.orgId);
  const generated = buildRecordDetailQuery({
    snapshot: shell.snapshot,
    objectApiName: input.objectApiName,
    role: viewer.role,
    recordId: input.recordId,
    allFields: input.allFields,
  });
  const data = await recordsRuntime().graphjin.execute<Record<string, unknown>>({
    operationName: generated.operationName,
    query: generated.query,
    variables: generated.variables,
    token: viewer.token,
  });
  const row = projectRecordRows(generated.view, rowsFrom(data[generated.resultField]))[0] ?? null;
  const readableKeys = new Set<string>([
    "id",
    "owner_user_id",
    ...shell.snapshot.fields
      .filter((field) => field.objectId === generated.view.object.id)
      .map((field) => String(field.apiName)),
  ]);
  const relationships = shell.snapshot.relationships ?? [];
  const relatedTargets = relationships.flatMap((relationship) => {
    if (relationship.toObjectId !== generated.view.object.id) return [];
    const source = shell.snapshot.objects.find(
      (candidate) => candidate.id === relationship.fromObjectId && candidate.archivedAt === null,
    );
    const field = shell.snapshot.fields.find(
      (candidate) => candidate.id === relationship.fromFieldId && candidate.archivedAt === null,
    );
    const readable =
      source &&
      viewerPermission(shell.snapshot, viewer.role, source.apiName, "read");
    return source && field && readable
      ? [{ relationship, source, field }]
      : [];
  });
  const [relatedLists, historyData, pendingByRecord] = row
    ? await Promise.all([
        Promise.all(
          relatedTargets.map(async ({ relationship, source, field }): Promise<RecordRelatedList> => {
            const relatedQuery = buildRecordListQuery({
              snapshot: shell.snapshot,
              objectApiName: source.apiName,
              role: viewer.role,
              userId: viewer.userId,
              first: 10,
              filters: [{ field: field.apiName, operator: "eq", value: input.recordId }],
            });
            const relatedData = await recordsRuntime().graphjin.execute<Record<string, unknown>>({
              operationName: relatedQuery.operationName,
              query: relatedQuery.query,
              variables: relatedQuery.variables,
              token: viewer.token,
            });
            const relatedRows = projectRecordRows(relatedQuery.view, rowsFrom(relatedData[relatedQuery.resultField]));
            return {
              label: relationship.label ?? source.pluralLabel,
              fieldApiName: field.apiName,
              view: relatedQuery.view,
              rows: relatedRows,
              owners: await resolveOwnerIdentities({
                orgId: input.orgId,
                appId: shell.snapshot.app.appId,
                rows: relatedRows,
              }),
              references: await resolveReferenceIdentities({
                snapshot: shell.snapshot,
                view: relatedQuery.view,
                rows: relatedRows,
                role: viewer.role,
                token: viewer.token,
              }),
            };
          }),
        ),
        (() => {
          const historyQuery = buildRecordChangeLogQuery({
            snapshot: shell.snapshot,
            objectApiName: input.objectApiName,
            role: viewer.role,
            recordId: input.recordId,
            first: 25,
          });
          return recordsRuntime().graphjin.execute<Record<string, unknown>>({
            operationName: historyQuery.operationName,
            query: historyQuery.query,
            variables: historyQuery.variables,
            token: viewer.token,
          }).then((history) =>
            readableHistory(rowsFrom(history[historyQuery.resultField]), readableKeys),
          );
        })(),
        resolvePendingRecordActions({
          orgId: input.orgId,
          appId: shell.snapshot.app.appId,
          objectApiName: generated.view.object.apiName,
          recordIds: [String(row.id)],
        }),
      ])
    : [[], [], {} as Record<string, PendingRecordAction[]>];
  return {
    row,
    view: generated.view,
    app: shell.snapshot.app,
    owners: await resolveOwnerIdentities({
      orgId: input.orgId,
      appId: shell.snapshot.app.appId,
      rows: row ? [row] : [],
    }),
    references: await resolveReferenceIdentities({
      snapshot: shell.snapshot,
      view: generated.view,
      rows: row ? [row] : [],
      role: viewer.role,
      token: viewer.token,
    }),
    layout:
      shell.snapshot.layouts.find(
        (layout) => layout.objectId === generated.view.object.id && layout.kind === "detail",
      )?.definition ?? null,
    relatedLists,
    pendingActions: row ? pendingByRecord[String(row.id)] ?? [] : [],
    history: historyData.flatMap((entry): RecordChangeLogEntry[] => {
      if (
        (typeof entry.id !== "string" && typeof entry.id !== "number") ||
        typeof entry.action !== "string" ||
        typeof entry.at !== "string" ||
        !entry.changes ||
        typeof entry.changes !== "object" ||
        Array.isArray(entry.changes)
      ) return [];
      return [{
        id: String(entry.id),
        action: entry.action,
        actorUserId: typeof entry.actor_user_id === "string" ? entry.actor_user_id : null,
        actionRequestId:
          typeof entry.action_request_id === "string" ? entry.action_request_id : null,
        changes: entry.changes as JsonObject,
        at: entry.at,
      }];
    }),
  };
}

export type RecordRecycleListResult = {
  rows: RecycledRecordSummary[];
  cursor: string | null;
  total: number;
  view: RecordObjectView;
  app: AppRegistrySnapshot["app"];
  owners: Record<string, RecordOwnerIdentity>;
};

export async function readRecordRecycleList(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
  first?: number;
  after?: string | null;
  search?: string | null;
}): Promise<RecordRecycleListResult> {
  const shell = await loadRecordAppShell(input.orgId, input.appId);
  if (shell.availability !== "active") {
    throw new RecordAppRouteError(
      shell.degradedReason ?? "record app is degraded",
      503,
    );
  }
  const viewer = await recordsViewer(input.orgId);
  const generated = buildRecordRecycleListQuery({
    snapshot: shell.snapshot,
    objectApiName: input.objectApiName,
    role: viewer.role,
    first: input.first,
    after: input.after,
    search: input.search,
  });
  const data = await recordsRuntime().graphjin.execute<Record<string, unknown>>({
    operationName: generated.operationName,
    query: generated.query,
    variables: generated.variables,
    token: viewer.token,
  });
  const rows = recycledRowsFrom(data[generated.resultField]);
  return {
    rows,
    cursor:
      typeof data[generated.cursorField] === "string"
        ? String(data[generated.cursorField])
        : null,
    total: totalFrom(data[generated.totalField]),
    view: generated.view,
    app: shell.snapshot.app,
    owners: await resolveOwnerIdentities({
      orgId: input.orgId,
      appId: shell.snapshot.app.appId,
      rows: rows.map((row) => ({ owner_user_id: row.ownerUserId })),
    }),
  };
}

export type RecordRecycleDetailResult = {
  row: RecycledRecordSummary | null;
  view: RecordObjectView;
  app: AppRegistrySnapshot["app"];
  owners: Record<string, RecordOwnerIdentity>;
};

export async function readRecordRecycleDetail(input: {
  orgId: string;
  appId: string;
  objectApiName: string;
  recordId: string;
}): Promise<RecordRecycleDetailResult> {
  const shell = await loadRecordAppShell(input.orgId, input.appId);
  if (shell.availability !== "active") {
    throw new RecordAppRouteError(
      shell.degradedReason ?? "record app is degraded",
      503,
    );
  }
  const viewer = await recordsViewer(input.orgId);
  const generated = buildRecordRecycleDetailQuery({
    snapshot: shell.snapshot,
    objectApiName: input.objectApiName,
    role: viewer.role,
    recordId: input.recordId,
  });
  const data = await recordsRuntime().graphjin.execute<Record<string, unknown>>({
    operationName: generated.operationName,
    query: generated.query,
    variables: generated.variables,
    token: viewer.token,
  });
  const row = recycledRowsFrom(data[generated.resultField])[0] ?? null;
  return {
    row,
    view: generated.view,
    app: shell.snapshot.app,
    owners: await resolveOwnerIdentities({
      orgId: input.orgId,
      appId: shell.snapshot.app.appId,
      rows: row ? [{ owner_user_id: row.ownerUserId }] : [],
    }),
  };
}

export async function getRecordSubstrateStatus(input: {
  orgId: string;
  appId: string;
}): Promise<{
  availability: "active" | "degraded";
  reason: string | null;
  config: JsonObject;
  unlinkedIdentities: number;
  latestSync: { watermark: unknown; updatedAt: string } | null;
}> {
  const shell = await loadRecordAppShellRaw(input.orgId, input.appId);
  const [identity, sync] = await Promise.all([
    recordsRuntime().pool.query<{ count: number }>(
      `select count(*)::int as count from engine.identity_map
       where org_id = $1 and app_id = $2 and status in ('unlinked', 'conflict')`,
      [input.orgId, shell.snapshot.app.appId],
    ),
    recordsRuntime().pool.query<{ watermark: unknown; updated_at: Date }>(
      `select watermark, updated_at from engine.sync_cursor
       where org_id = $1 and app_id = $2
       order by updated_at desc limit 1`,
      [input.orgId, shell.snapshot.app.appId],
    ),
  ]);
  const latest = sync.rows[0];
  return {
    availability: shell.availability,
    reason: shell.degradedReason,
    config: shell.config,
    unlinkedIdentities: identity.rows[0]?.count ?? 0,
    latestSync: latest
      ? { watermark: latest.watermark, updatedAt: latest.updated_at.toISOString() }
      : null,
  };
}
