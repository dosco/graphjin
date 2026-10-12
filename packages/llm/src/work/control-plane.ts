import { enqueue, QUEUE } from "@neko/db/jobs";
import { holds } from "@neko/db";
import { actionKindIsGranted, entitlementActorForRun, runWorkflowFilter } from "./entitlement-scope";
import {
  createActionRequest,
  getActionRequest,
  listActionExecutions,
  listAllPolicies,
  listEnabledPolicies,
  upsertActionPolicyByName,
  type ActionPolicyRecord,
  type CreateActionPolicyInput,
  type UpsertActionPolicyResult,
} from "../workflows/action-store";
import type { ActionExecutionOutcome } from "../workflows/action-executor";
import { evaluateActionPolicy } from "../workflows/policy-engine";
import { withActionTargets } from "../workflows/action-targets";
import {
  saveWorkflowWithTrigger,
  type SaveWorkflowWithTriggerResult,
} from "../workflows/save-workflow-with-trigger";
import {
  deleteWorkflow,
  emitWorkflowOutput,
  listSubscriptionsByWorkflow,
  getWorkflow,
  listWorkflows,
  type SaveWorkflowInput,
  type WorkflowRecord,
  type WorkflowOutputInput,
  type WorkflowOutputRecord,
} from "../workflows/store";
import { notifyWorkflowOutputDeliveryHook } from "../workflows/output-delivery";
import { rememberWorkMemory, searchWorkMemoryByContext } from "./memory";
import { searchLibraryForRun } from "./library";
import type {
  GraphjinAgentResponse,
  GraphjinAgentStatus,
} from "../graphjin/agent";
import {
  callGraphjinMcpTool as callRemoteGraphjinMcpTool,
  listGraphjinMcpTools as listRemoteGraphjinMcpTools,
} from "../graphjin/mcp-client";
import { assertGraphjinMutationAllowed } from "../graphjin/sources-config";
import type {
  CallToolResult,
  Tool,
} from "@modelcontextprotocol/sdk/types.js";
import type { OpenApiSpecAssetView } from "../graphjin/openapi-assets";
import type {
  JsonObject,
  RecordListFilter,
  RecordsCatalogResult,
  RecordsDetailResult,
  RecordsQueryResult,
  RecordsRecycleDetailResult,
  RecordsRecycleQueryResult,
  RecordsReadGateway,
  RecordsViewer,
} from "@neko/records";

type PolicyRequestSubject = Parameters<typeof evaluateActionPolicy>[0];
type PolicyDecision = ReturnType<typeof evaluateActionPolicy>;
type CreateActionRequestInput = Parameters<typeof createActionRequest>[0];
type RememberWorkMemoryInput = Parameters<typeof rememberWorkMemory>[0];
type WorkMemorySearchArgs = Parameters<typeof searchWorkMemoryByContext>[0];
type WorkMemorySearchResult = Awaited<
  ReturnType<typeof searchWorkMemoryByContext>
>[number];
type LibrarySearchArgs = Parameters<typeof searchLibraryForRun>[0];
type LibrarySearchResult = Awaited<
  ReturnType<typeof searchLibraryForRun>
>[number];

/**
 * JSON-safe view of a control-plane result: what a value looks like after a
 * broker HTTP hop (Dates become ISO strings, undefined drops). The in-process
 * impl applies the same transform so both paths return identical shapes.
 */
export type Wire<T> = T extends Date
  ? string
  : T extends Array<infer U>
    ? Array<Wire<U>>
    : T extends object
      ? { [K in keyof T]: Wire<T[K]> }
      : T;

function toWire<T>(value: T): Wire<T> {
  return JSON.parse(JSON.stringify(value ?? null)) as Wire<T>;
}

export async function createActionRequestViaWorker(
  baseUrl: string,
  input: CreateActionRequestInput,
  fetchImpl: typeof fetch = fetch,
): Promise<{ id: string; status: string }> {
  const url = `${baseUrl.replace(/\/$/, "")}/admin/action-requests/create`;
  const response = await fetchImpl(url, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(input),
    signal: AbortSignal.timeout(65_000),
  });
  const body = (await response.json().catch(() => null)) as
    | { id?: unknown; status?: unknown; error?: unknown }
    | null;
  if (!response.ok) {
    const message =
      body && typeof body.error === "string"
        ? body.error
        : `worker action preflight failed with status ${response.status}`;
    throw new Error(message);
  }
  if (
    !body ||
    typeof body.id !== "string" ||
    !body.id ||
    typeof body.status !== "string"
  ) {
    throw new Error("worker action preflight returned an invalid response");
  }
  return { id: body.id, status: body.status };
}

export function assertReadOnlyGraphql(query: string): void {
  const text = query.trim();
  if (!text) throw new Error("GraphJin read query is empty");
  // Deliberately conservative: rejecting the words even in a string/comment
  // can cause a harmless false positive, while accepting either operation
  // would turn the broker into a write bypass for legacy GraphJin sources.
  if (/\b(?:mutation|subscription)\b/i.test(text)) {
    throw new Error("GraphJin agent broker allows query operations only");
  }
  if (!text.startsWith("{") && !/^query\b/i.test(text)) {
    throw new Error("GraphJin agent broker requires an explicit query operation");
  }
}

/** A run's GraphJin token: the K1 principal plus group roles when group grants are on. */
/** The org's knowledge pack digest, so GraphJin's agent starts from known patterns and join paths. */
async function graphjinAgentKnowledge(orgId: string): Promise<{ knowledge_pack?: Record<string, unknown> }> {
  try {
    const { ensureOrgWorkspace } = await import("./workspace");
    const { knowledgePackPaths, readKnowledgePack } = await import("../knowledge-pack");
    const pack = await readKnowledgePack(knowledgePackPaths((await ensureOrgWorkspace(orgId)).knowledgeRoot));
    if (pack.mode !== "agentic") return {};
    const insights = JSON.parse(pack.insights) as { hub_tables?: unknown; help_cards?: unknown };
    return {
      knowledge_pack: {
        graphjin_syntax: JSON.parse(pack.syntax),
        hub_tables: insights.hub_tables,
        help_cards: insights.help_cards,
      },
    };
  } catch {
    return {};
  }
}

async function runGraphjinToken(orgId: string, runId: string | null | undefined): Promise<string> {
  const { mintGraphjinToken } = await import("../graphjin/token");
  const { getWorkRunActor } = await import("./personas");
  const principal = runId ? graphjinReadPrincipal(await getWorkRunActor(runId, orgId)) : { userId: null, role: "service" as const };
  let claims: { roles: string[]; groups: string[] } | null = null;
  if (principal.userId && principal.role === "member") {
    const { getGroupGrantsEnabled, graphjinGroupClaims } = await import("@neko/db");
    if (await getGroupGrantsEnabled(orgId)) claims = await graphjinGroupClaims(orgId, principal.userId);
  }
  return mintGraphjinToken({
    orgId,
    userId: principal.userId,
    role: principal.role,
    ...(claims ? { groupRoles: claims.roles, groups: claims.groups } : {}),
  });
}

export function graphjinReadPrincipal(actor: {
  userId: string | null;
  role: string | null;
}): { userId: string | null; role: "admin" | "member" | "service" } {
  if (actor.role === "admin" || actor.role === "member") {
    return { userId: actor.userId, role: actor.role };
  }
  if (actor.role === "service") {
    return { userId: null, role: "service" };
  }
  throw new Error("GraphJin read actor has an invalid role");
}

export function graphjinDevelopmentAuthHeaders(
  principal: ReturnType<typeof graphjinReadPrincipal>,
  nodeEnv = process.env.NODE_ENV,
): Record<string, string> {
  if (nodeEnv === "production") {
    throw new Error(
      "GraphJin development header auth is forbidden in production",
    );
  }
  return {
    "X-User-ID": principal.userId ?? "openneko-service",
    "X-User-Role": principal.role,
  };
}

/** A run needs at least one data_source item to reach GraphJin. */
async function assertRunHoldsDataSource(orgId: string, runId: string | null | undefined): Promise<void> {
  if (!runId) return;
  const actor = await entitlementActorForRun(orgId, runId);
  const { heldItems } = await import("@neko/db");
  const held = actor ? await heldItems(actor, "data_source") : new Set<string>();
  if (held !== "*" && held.size === 0) throw new Error("No data source is available to this run.");
}

async function graphjinMcpAccess(input: {
  orgId: string;
  runId?: string | null;
}): Promise<{ mcpUrl: string; headers: Record<string, string> }> {
  await assertRunHoldsDataSource(input.orgId, input.runId);
  const { and, data_source, db, desc, eq } = await import("@neko/db");
  const [source] = await db()
    .select({
      mcpUrl: data_source.mcp_url,
      graphqlUrl: data_source.graphql_url,
      kind: data_source.kind,
      authMode: data_source.auth_mode,
    })
    .from(data_source)
    .where(
      and(
        eq(data_source.org_id, input.orgId),
        eq(data_source.enabled, true),
      ),
    )
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);
  if (!source?.mcpUrl) {
    throw new Error("no enabled GraphJin MCP endpoint configured");
  }
  if (source.kind !== "graphjin") {
    throw new Error("the default enabled data source is not a GraphJin server");
  }
  if (
    isInternalGraphjinURL(source.graphqlUrl) ||
    isInternalGraphjinURL(source.mcpUrl)
  ) {
    throw new Error("refusing to expose the OpenNeko internal GraphJin MCP");
  }

  const headers: Record<string, string> = {};
  if (source.authMode === "jwt") {
    headers.authorization = `Bearer ${await runGraphjinToken(input.orgId, input.runId)}`;
  } else if (source.authMode === "development") {
    let principal: ReturnType<typeof graphjinReadPrincipal> = {
      userId: null,
      role: "service",
    };
    if (input.runId) {
      const { getWorkRunActor } = await import("./personas");
      principal = graphjinReadPrincipal(
        await getWorkRunActor(input.runId, input.orgId),
      );
    }
    Object.assign(headers, graphjinDevelopmentAuthHeaders(principal));
  }
  return { mcpUrl: source.mcpUrl, headers };
}

async function requireSourceConfigAccess(input: {
  orgId: string;
  runId?: string | null;
  actorRole?: string | null;
}): Promise<
  { ok: true; userId: string | null } | { ok: false; error: string }
> {
  const {
    and,
    app_user,
    db,
    eq,
    getGraphjinConfigSettingsForOrg,
    work_run,
  } = await import("@neko/db");
  const settings = await getGraphjinConfigSettingsForOrg(input.orgId);
  if (!settings.sourceConfigEnabled) {
    return {
      ok: false,
      error: "GraphJin config capability is disabled.",
    };
  }

  let role: string | null = null;
  let userId: string | null = null;
  if (input.runId) {
    const [run] = await db()
      .select({
        orgId: work_run.org_id,
        userId: work_run.actor_user_id,
        snapshotRole: work_run.actor_role,
      })
      .from(work_run)
      .where(eq(work_run.id, input.runId))
      .limit(1);
    if (!run || run.orgId !== input.orgId) {
      return { ok: false, error: "GraphJin config run is not available." };
    }
    if (run.userId) {
      userId = run.userId;
      const [user] = await db()
        .select({ disabledAt: app_user.disabled_at })
        .from(app_user)
        .where(
          and(
            eq(app_user.id, run.userId),
            eq(app_user.org_id, input.orgId),
          ),
        )
        .limit(1);
      if (user && !user.disabledAt) {
        const { resolveUserGroups } = await import("@neko/db");
        role = (await resolveUserGroups(input.orgId, run.userId)).administrator ? "admin" : "member";
      } else {
        role = null;
      }
    } else {
      // Null-user admin runs are valid only in the solo profile. In a
      // multi-user org, a deleted/detached admin must not inherit the solo
      // profile's authority from the role snapshot.
      const [anyUser] = await db()
        .select({ id: app_user.id })
        .from(app_user)
        .where(eq(app_user.org_id, input.orgId))
        .limit(1);
      role = anyUser ? null : run.snapshotRole;
    }
  } else {
    role = input.actorRole ?? null;
  }
  if (role !== "admin") {
    return {
      ok: false,
      error: "GraphJin config tools are admin-only.",
    };
  }

  return { ok: true, userId };
}

export type SourceConfigAgentResult = {
  denied?: boolean;
  error?: string;
  source?: { id: string; name: string; host: string | null };
  agentStatus?: GraphjinAgentStatus;
  response?: GraphjinAgentResponse;
};

/** Read-only answer from the selected customer GraphJin server agent. */
export type GraphjinDataAgentResult = {
  denied?: boolean;
  error?: string;
  source?: { id: string; name: string; host: string | null };
  agentStatus?: GraphjinAgentStatus;
  response?: GraphjinAgentResponse;
};

export type SourceConfigPreviewResult = {
  denied?: boolean;
  error?: string;
  source?: { id: string; name: string; host: string | null };
  specAsset?: OpenApiSpecAssetView;
  preview?: {
    valid: boolean;
    patchHash: string;
    catalogRevision: string;
    expiresAt: string | null;
    scope: string | null;
    reloadMode: string | null;
    changes: unknown;
    findings: unknown;
    errors: unknown;
  };
};

export type WorkflowListEntry = Wire<WorkflowRecord> & {
  /** Enabled source_change trigger filter, if any. */
  when: Record<string, unknown> | null;
};

export type WaitForActionExecutionResult =
  | {
      status: "succeeded";
      outcome: ActionExecutionOutcome;
    }
  | {
      status: "failed";
      error: string;
    }
  | {
      status: "timeout";
    };

type RecordsControlPlaneRuntime = {
  gateway: RecordsReadGateway;
};

const recordsRuntimeGlobal = globalThis as typeof globalThis & {
  __opennekoLlmRecordsRuntime?: Promise<RecordsControlPlaneRuntime>;
};

async function recordsControlPlaneRuntime(): Promise<RecordsControlPlaneRuntime> {
  const existing = recordsRuntimeGlobal.__opennekoLlmRecordsRuntime;
  if (existing) return existing;
  const created = Promise.all([
    import("pg"),
    import("@neko/db"),
    import("@neko/records"),
  ]).then(([pgModule, dbModule, records]) => {
    const pool = new pgModule.default.Pool({
      ...dbModule.buildRecordsPoolConfig(),
      application_name: "openneko-agent-records",
    });
    return {
      gateway: new records.RecordsReadGateway(
        pool,
        new records.RecordRegistry(pool),
        new records.RecordsGraphjinClient({
          baseUrl:
            process.env.OPENNEKO_RECORDS_GRAPHJIN_URL ??
            "http://127.0.0.1:8090",
        }),
      ),
    };
  });
  recordsRuntimeGlobal.__opennekoLlmRecordsRuntime = created;
  return created;
}

async function recordsViewerForRun(input: {
  orgId: string;
  runId: string;
}): Promise<RecordsViewer> {
  const {
    and,
    app_user,
    db,
    eq,
    resolveUserGroups,
    work_run,
  } = await import("@neko/db");
  const [run] = await db()
    .select({
      orgId: work_run.org_id,
      userId: work_run.actor_user_id,
      snapshotRole: work_run.actor_role,
    })
    .from(work_run)
    .where(eq(work_run.id, input.runId))
    .limit(1);
  if (!run || run.orgId !== input.orgId) {
    throw new Error("records tools are not available to this run");
  }

  if (run.userId) {
    const [user] = await db()
      .select({ disabledAt: app_user.disabled_at })
      .from(app_user)
      .where(
        and(eq(app_user.id, run.userId), eq(app_user.org_id, input.orgId)),
      )
      .limit(1);
    if (!user || user.disabledAt) {
      throw new Error("records tools are not available to this actor");
    }
    const groups = await resolveUserGroups(input.orgId, run.userId);
    return {
      orgId: input.orgId,
      userId: run.userId,
      role: groups.administrator ? "admin" : "member",
      groupIds: [...groups.groupIds, ...groups.ssoGroupIds],
      solo: false,
    };
  }

  // A userless admin exists only in the solo profile. Once the org has any
  // user, a detached/deleted run must not inherit this synthetic authority.
  const [anyUser] = await db()
    .select({ id: app_user.id })
    .from(app_user)
    .where(eq(app_user.org_id, input.orgId))
    .limit(1);
  if (anyUser || run.snapshotRole !== "admin") {
    throw new Error("records tools are not available to this actor");
  }
  return {
    orgId: input.orgId,
    userId: `urn:openneko:solo-admin:${input.orgId}`,
    role: "admin",
    groupIds: [],
    solo: true,
  };
}

async function activeRecordAppIds(orgId: string): Promise<ReadonlySet<string>> {
  const { and, app_state, db, eq } = await import("@neko/db");
  const rows = await db()
    .select({ appId: app_state.app_id })
    .from(app_state)
    .where(and(eq(app_state.org_id, orgId), eq(app_state.status, "active")));
  return new Set(rows.map((row) => row.appId));
}

/** The payload lets kinds with a target spec match their real targets. */
export type PolicyEvaluationInput = { orgId: string; payload?: Record<string, unknown> | null } & PolicyRequestSubject;

/**
 * The narrow control-plane surface an agent turn touches: policy eval,
 * action-request create + enqueue, the two memory ops, and the builder
 * tools' workflow/rule saves and lists. In-process today (direct DB/pg-boss).
 * The agent-sandbox path (Phase 2) injects an HTTP impl backed by the broker,
 * so the sandbox never holds DB creds or the pg-boss connection — the worker
 * stays the only gateway to those.
 */
export interface AgentControlPlane {
  evaluateActionPolicy(
    input: PolicyEvaluationInput,
  ): Promise<PolicyDecision>;
  createActionRequest(input: CreateActionRequestInput): Promise<{ id: string; status: string }>;
  enqueueActionExecute(input: {
    orgId: string;
    actionRequestId: string;
  }): Promise<void>;
  /**
   * Wait for a worker-owned action adapter to finish without exposing the
   * worker or database to the agent sandbox. Auto-mode read actions use this
   * so their actual result returns to the model in the same tool call.
   */
  waitForActionExecution(input: {
    orgId: string;
    actionRequestId: string;
    timeoutMs?: number;
  }): Promise<WaitForActionExecutionResult>;
  rememberWorkMemory(input: RememberWorkMemoryInput): Promise<{ id: string }>;
  updateBotInstructions(input: {
    orgId: string;
    threadId: string;
    instructions: string;
  }): Promise<{ ok: boolean }>;
  searchSkillsForRun(input: {
    orgId: string;
    runId: string;
    query: string;
    limit?: number;
  }): Promise<Array<{ name: string; score: number }>>;
  searchWorkMemoryByContext(
    args: WorkMemorySearchArgs,
  ): Promise<WorkMemorySearchResult[]>;
  searchLibraryForRun(args: LibrarySearchArgs): Promise<LibrarySearchResult[]>;
  queryGraphjinRead(input: {
    orgId: string;
    /** Trusted work-run binding used to mint the same actor identity as chat. */
    runId?: string | null;
    query: string;
    variables?: Record<string, unknown>;
    operationName?: string;
  }): Promise<{
    data: unknown;
    errors?: Array<{ message: string; path?: (string | number)[] }>;
  }>;
  /** Complete caller-visible tool catalog from the configured GraphJin MCP. */
  listGraphjinTools(input: {
    orgId: string;
    runId?: string | null;
  }): Promise<Tool[]>;
  /** Invoke one caller-visible GraphJin MCP tool with its native arguments. */
  callGraphjinTool(input: {
    orgId: string;
    runId?: string | null;
    name: string;
    arguments?: Record<string, unknown>;
  }): Promise<CallToolResult>;
  /**
   * Delegate one bounded read-only data question to a customer GraphJin
   * server agent. Source selection and credentials remain on the host.
   */
  askGraphjinDataAgent(input: {
    orgId: string;
    runId?: string | null;
    dataSourceId?: string;
    instruction: string;
    maxSteps?: number;
  }): Promise<GraphjinDataAgentResult>;
  /** Actor-scoped registry catalog for native generated records apps. */
  listRecordCatalog(input: {
    orgId: string;
    runId: string;
    appId?: string;
  }): Promise<RecordsCatalogResult>;
  /** Generated, bounded GraphJin list/search query under the run's actor. */
  findRecords(input: {
    orgId: string;
    runId: string;
    appId: string;
    objectApiName: string;
    first?: number;
    after?: string | null;
    sort?: { field: string; direction: "asc" | "desc" };
    search?: string | null;
    filters?: RecordListFilter[];
    myRecords?: boolean;
  }): Promise<RecordsQueryResult>;
  /** Exact actor-scoped record lookup after an id has been resolved. */
  getRecord(input: {
    orgId: string;
    runId: string;
    appId: string;
    objectApiName: string;
    recordId: string;
    allFields?: boolean;
  }): Promise<RecordsDetailResult>;
  /** Actor-scoped deleted-record summaries for safe restore id resolution. */
  findRecycledRecords(input: {
    orgId: string;
    runId: string;
    appId: string;
    objectApiName: string;
    first?: number;
    after?: string | null;
    search?: string | null;
  }): Promise<RecordsRecycleQueryResult>;
  /** Exact actor-scoped lookup in the recycle index. */
  getRecycledRecord(input: {
    orgId: string;
    runId: string;
    appId: string;
    objectApiName: string;
    recordId: string;
  }): Promise<RecordsRecycleDetailResult>;
  /** Shipped domain blueprints; exact lookup includes its app_create payload. */
  listRecordBlueprints(input: {
    orgId: string;
    blueprintId?: string;
  }): Promise<{
    blueprints: Array<{
      id: string;
      version: string;
      description: string;
      payload?: JsonObject;
    }>;
  }>;
  saveWorkflowWithTrigger(
    input: SaveWorkflowInput,
  ): Promise<Wire<SaveWorkflowWithTriggerResult>>;
  emitWorkflowOutput(
    input: WorkflowOutputInput,
  ): Promise<Wire<WorkflowOutputRecord>>;
  listWorkflowsWithTriggers(input: {
    orgId: string;
    limit?: number;
    runId?: string | null;
  }): Promise<{ total: number; workflows: WorkflowListEntry[] }>;
  /**
   * Hard-delete a workflow and its dependents (triggers, runs, outputs,
   * proposed actions cascade via FK). Org-scoped: `found: false` when the id
   * doesn't belong to the org. Mirrors `DELETE /api/workflows/[id]`.
   */
  deleteWorkflow(input: {
    orgId: string;
    workflowId: string;
    runId?: string | null;
  }): Promise<{ found: boolean; name: string | null }>;
  upsertActionPolicyByName(
    input: CreateActionPolicyInput,
  ): Promise<Wire<UpsertActionPolicyResult>>;
  listActionPolicies(input: {
    orgId: string;
    limit?: number;
  }): Promise<{ total: number; policies: Array<Wire<ActionPolicyRecord>> }>;
  /** ADM3: installed plugins (manifest) + marketplace catalog. */
  listPlugins(input: { orgId: string }): Promise<PluginCatalog>;
  /** ADM1: the org's users (id, email, role, disabled). */
  listGroups(input: { orgId: string }): Promise<{
    groups: Array<{
      id: string;
      slug: string;
      name: string;
      kind: string;
      memberCount: number;
      members: Array<{ userId: string; email: string; sources: string[] }>;
      grants: Array<{ itemType: string; itemId: string }>;
      dataAccess: Array<{ id: string; source: string; table: string; columns: string[]; rowFilter: unknown }>;
    }>;
    idpRules: Array<{ id: string; idpGroupName: string; ssoGroupId: string; userGroupId: string; userGroupName: string }>;
    groupDataAccessEnabled: boolean;
  }>;
  listUsers(input: { orgId: string }): Promise<{
    users: Array<{
      id: string;
      email: string;
      name: string | null;
      role: string;
      disabledAt: string | null;
      lastLoginAt: string | null;
      groupIds?: string[];
    }>;
    groups?: Array<{
      id: string;
      provider: string;
      tenantId: string;
      externalId: string;
      displayName: string | null;
      active: boolean;
    }>;
  }>;
  /**
   * ADM2: the org's data-source registry. Connection URLs are reduced to
   * hostnames — credentials never enter model context.
   */
  listDataSources(input: { orgId: string }): Promise<{
    sources: Array<{
      id: string;
      name: string;
      label: string | null;
      kind: string;
      authMode: string;
      isDefault: boolean;
      enabled: boolean;
      host: string | null;
      hasMcp: boolean;
    }>;
  }>;
  /**
   * OL5: the live "unified source graph" of the CUSTOMER GraphJin —
   * databases, capabilities, and namespaces discovered from gj_catalog
   * under a minted service token. Read-only; never touches the OpenNeko
   * internal GraphJin. `reachable: false` with `error` when the engine
   * is down or not in sources mode.
   */
  describeSourceGraph(input: {
    orgId: string;
    runId?: string | null;
    actorRole?: string | null;
  }): Promise<{
    reachable: boolean;
    denied?: boolean;
    error?: string;
    sourceName?: string;
    host?: string | null;
    databases?: Array<{ id: string; name: string; summary: string }>;
    capabilities?: Array<{ id: string; name: string; summary: string }>;
    namespaces?: Array<{ id: string; name: string; summary: string }>;
  }>;
  /**
   * OL5: the NAMES of stored data-source connection secrets (never the
   * values). The agent references these as `secretRef` when proposing a
   * source registration; the worker resolves the value at apply.
   */
  listSourceSecretNames(input: {
    orgId: string;
    runId?: string | null;
    actorRole?: string | null;
  }): Promise<{
    denied?: boolean;
    error?: string;
    names: Array<{ name: string; description: string | null; updatedAt: string }>;
  }>;
  /** Import a hosted OpenAPI document through the trusted SSRF-safe host. */
  importOpenApiSpec(input: {
    orgId: string;
    runId?: string | null;
    actorRole?: string | null;
    url: string;
    baseUrl?: string | null;
  }): Promise<{
    denied?: boolean;
    error?: string;
    asset?: OpenApiSpecAssetView;
  }>;
  /** List only the current org's managed OpenAPI asset metadata. */
  listOpenApiSpecs(input: {
    orgId: string;
    runId?: string | null;
    actorRole?: string | null;
    limit?: number;
  }): Promise<{
    denied?: boolean;
    error?: string;
    assets: OpenApiSpecAssetView[];
  }>;
  /**
   * Ask the selected customer GraphJin's built-in server agent to inspect or
   * explain its configuration. The host verifies the agent is globally
   * read-only before forwarding an instruction, and authenticates with a
   * short-lived admin JWT that never enters the sandbox.
   */
  askSourceConfigAgent(input: {
    orgId: string;
    runId?: string | null;
    actorRole?: string | null;
    dataSourceId?: string;
    instruction: string;
    maxSteps?: number;
  }): Promise<SourceConfigAgentResult>;
  /** Validate the exact allowlisted patch before an approval is created. */
  previewSourceConfigChange(input: {
    orgId: string;
    runId?: string | null;
    actorRole?: string | null;
    dataSourceId?: string;
    payload: Record<string, unknown>;
  }): Promise<SourceConfigPreviewResult>;
  /**
   * ADM4: the audit trail, for ADMIN runs only — the requesting run's
   * K1 actor is checked server-side (the sandbox is never trusted to
   * assert its own role). Member/service runs get denied: true.
   */
  listAuditTrail(input: { orgId: string; runId?: string | null; limit?: number }): Promise<{
    denied?: boolean;
    requests?: Array<{
      id: string;
      kind: string;
      target: string | null;
      status: string;
      scope: string;
      summary: string | null;
      actorUserId: string | null;
      actorRole: string | null;
      actorBackend: string | null;
      createdAt: string;
    }>;
    alerts?: Array<{
      id: string;
      kind: string;
      subject: string;
      observed: number;
      threshold: number;
      windowSeconds: number;
      acknowledgedAt: string | null;
      createdAt: string;
    }>;
    gatewaySummary?: Array<{
      runId: string | null;
      backend: string | null;
      actorRole: string | null;
      calls: number;
    }>;
  }>;
  /** ADM5: channel workspaces (CH2) + identities (CH3) for chat-first channel management. */
  listChannels(input: { orgId: string }): Promise<{
    workspaces: Array<{
      channelPlugin: string;
      workspaceId: string;
      createdAt: string | null;
    }>;
    identities: Array<{
      id: string;
      channelPlugin: string;
      workspaceId: string;
      channelUserId: string;
      displayName: string | null;
      email: string | null;
      status: string;
      appUserId: string | null;
      firstSeenAt: string | null;
      verifiedAt: string | null;
    }>;
  }>;
}

export type PluginCatalog = {
  installed: Array<{
    name: string;
    version: string;
    source: string;
    network: string[];
    installedAt: string | null;
  }>;
  available: Array<{
    name: string;
    title: string;
    description: string;
    version: string;
  }>;
  marketplaceError?: string;
};

export class InProcessControlPlane implements AgentControlPlane {
  async evaluateActionPolicy(
    input: PolicyEvaluationInput,
  ): Promise<PolicyDecision> {
    const { orgId, payload, ...subject } = input;
    const policies = await listEnabledPolicies(orgId);
    return evaluateActionPolicy(await withActionTargets(orgId, subject, payload), policies);
  }

  async createActionRequest(
    input: CreateActionRequestInput,
  ): Promise<{ id: string; status: string }> {
    if (input.kind === "source_config_admin") {
      const gate = await requireSourceConfigAccess({
        orgId: input.orgId,
        runId: input.workRunId ?? null,
        actorRole: input.actorRole ?? null,
      });
      if (!gate.ok) {
        throw new Error(`source_config_admin: ${gate.error}`);
      }
    }
    if (input.workRunId && (await actionKindIsGranted(input.orgId, input.kind, input.scope))) {
      const actor = await entitlementActorForRun(input.orgId, input.workRunId);
      if (!actor || !(await holds(actor, "action", input.kind)).allowed) {
        throw new Error(`action ${input.kind} is not available to this run`);
      }
    }
    const workerAdminUrl = process.env.WORKER_ADMIN_URL?.trim();
    if (workerAdminUrl) {
      return createActionRequestViaWorker(workerAdminUrl, input);
    }
    const request = await createActionRequest(input);
    return { id: request.id, status: request.status };
  }

  async enqueueActionExecute(input: {
    orgId: string;
    actionRequestId: string;
  }): Promise<void> {
    await enqueue(QUEUE.ACTION_EXECUTE, input);
  }

  async waitForActionExecution(input: {
    orgId: string;
    actionRequestId: string;
    timeoutMs?: number;
  }): Promise<WaitForActionExecutionResult> {
    const timeoutMs = Math.min(
      Math.max(input.timeoutMs ?? 45_000, 1_000),
      120_000,
    );
    const deadline = Date.now() + timeoutMs;

    while (Date.now() < deadline) {
      const request = await getActionRequest(
        input.orgId,
        input.actionRequestId,
      );
      if (!request) {
        return {
          status: "failed",
          error: "Action request disappeared before execution completed.",
        };
      }

      if (request.status === "executed") {
        const [execution] = await listActionExecutions(request.id);
        if (execution?.status === "partially_applied" || execution?.status === "reconcile_required") {
          return {
            status: "failed",
            error: execution.status === "partially_applied"
              ? "The action was partly applied. Review its details before any retry."
              : "The action outcome needs reconciliation. Review its details before any retry.",
          };
        }
        return {
          status: "succeeded",
          outcome: {
            result: execution?.result ?? null,
            externalRef: execution?.externalRef ?? null,
            commandOrOperation: execution?.commandOrOperation ?? null,
          },
        };
      }

      if (request.status === "failed" || request.status === "rejected") {
        const [execution] = await listActionExecutions(request.id);
        return {
          status: "failed",
          error:
            execution?.error ??
            request.rejectionReason ??
            (request.status === "rejected"
              ? "Action request was rejected."
              : "Action execution failed."),
        };
      }

      await new Promise((resolve) => setTimeout(resolve, 250));
    }

    return { status: "timeout" };
  }

  async rememberWorkMemory(
    input: RememberWorkMemoryInput,
  ): Promise<{ id: string }> {
    const memory = await rememberWorkMemory(input);
    return { id: memory.id };
  }

  async searchWorkMemoryByContext(
    args: WorkMemorySearchArgs,
  ): Promise<WorkMemorySearchResult[]> {
    return searchWorkMemoryByContext(args);
  }

  async updateBotInstructions(input: {
    orgId: string;
    threadId: string;
    instructions: string;
  }): Promise<{ ok: boolean }> {
    const { updateThreadBotInstructions } = await import("./bot");
    return updateThreadBotInstructions(input);
  }

  async searchSkillsForRun(input: {
    orgId: string;
    runId: string;
    query: string;
    limit?: number;
  }): Promise<Array<{ name: string; score: number }>> {
    const { searchSkillsForRun } = await import("./skill-index");
    return searchSkillsForRun(input);
  }

  async searchLibraryForRun(
    args: LibrarySearchArgs,
  ): Promise<LibrarySearchResult[]> {
    return searchLibraryForRun(args);
  }

  async queryGraphjinRead(input: {
    orgId: string;
    runId?: string | null;
    query: string;
    variables?: Record<string, unknown>;
    operationName?: string;
  }) {
    assertReadOnlyGraphql(input.query);
    await assertRunHoldsDataSource(input.orgId, input.runId);
    const { data_source, db, desc, eq } = await import("@neko/db");
    const [source] = await db()
      .select({
        graphqlUrl: data_source.graphql_url,
        authMode: data_source.auth_mode,
      })
      .from(data_source)
      .where(eq(data_source.org_id, input.orgId))
      .orderBy(desc(data_source.is_default), data_source.created_at)
      .limit(1);
    if (!source?.graphqlUrl) {
      throw new Error("no GraphJin GraphQL endpoint configured");
    }

    const headers: Record<string, string> = {};
    if (source.authMode === "jwt") {
      headers.authorization = `Bearer ${await runGraphjinToken(input.orgId, input.runId)}`;
    }
    const { graphjinQuery } = await import("../graphjin/client");
    return graphjinQuery({
      baseUrl: source.graphqlUrl,
      query: input.query,
      variables: input.variables,
      operationName: input.operationName,
      headers,
      signal: AbortSignal.timeout(60_000),
    });
  }

  async listGraphjinTools(input: {
    orgId: string;
    runId?: string | null;
  }): Promise<Tool[]> {
    const access = await graphjinMcpAccess(input);
    return listRemoteGraphjinMcpTools({
      baseUrl: access.mcpUrl,
      headers: access.headers,
      signal: AbortSignal.timeout(60_000),
    });
  }

  async callGraphjinTool(input: {
    orgId: string;
    runId?: string | null;
    name: string;
    arguments?: Record<string, unknown>;
  }): Promise<CallToolResult> {
    if (input.name === "execute_graphql") {
      await assertGraphjinMutationAllowed(
        String(input.arguments?.query ?? ""),
        process.env.OPENNEKO_GRAPHJIN_CONFIG,
      );
    }
    if (input.name === "execute_saved_query" && input.runId) {
      const actor = await entitlementActorForRun(input.orgId, input.runId);
      const name = String(input.arguments?.name ?? "");
      if (!actor || !(await holds(actor, "saved_query", name)).allowed) {
        return {
          isError: true,
          content: [{ type: "text", text: `Saved query "${name}" is not available to this run.` }],
        };
      }
    }
    const access = await graphjinMcpAccess(input);
    return callRemoteGraphjinMcpTool(
      {
        baseUrl: access.mcpUrl,
        headers: access.headers,
        // Native tools include ask_graphjin_agent and governed operations that
        // legitimately outlive a normal query. Match the existing agent path.
        signal: AbortSignal.timeout(180_000),
      },
      { name: input.name, arguments: input.arguments },
    );
  }

  async askGraphjinDataAgent(input: {
    orgId: string;
    runId?: string | null;
    dataSourceId?: string;
    instruction: string;
    maxSteps?: number;
  }): Promise<GraphjinDataAgentResult> {
    try {
      await assertRunHoldsDataSource(input.orgId, input.runId);
    } catch (error) {
      return { denied: true, error: error instanceof Error ? error.message : String(error) };
    }
    const instruction = input.instruction.trim();
    if (!instruction || instruction.length > 8_000) {
      return { error: "instruction must contain between 1 and 8,000 characters" };
    }

    const { and, data_source, db, desc, eq } = await import("@neko/db");
    const rows = await db()
      .select({
        id: data_source.id,
        name: data_source.name,
        kind: data_source.kind,
        graphqlUrl: data_source.graphql_url,
        authMode: data_source.auth_mode,
      })
      .from(data_source)
      .where(
        and(
          eq(data_source.org_id, input.orgId),
          eq(data_source.enabled, true),
          ...(input.dataSourceId
            ? [eq(data_source.id, input.dataSourceId)]
            : []),
        ),
      )
      .orderBy(desc(data_source.is_default), data_source.created_at)
      .limit(input.dataSourceId ? 1 : 2);
    if (rows.length === 0) {
      return { error: "no enabled customer GraphJin data source was found" };
    }
    if (!input.dataSourceId && rows.length > 1) {
      return {
        error:
          "multiple enabled data sources exist; choose a dataSourceId before asking the data agent",
      };
    }
    const src = rows[0]!;
    if (src.kind !== "graphjin") {
      return { error: "the selected data source is not a GraphJin server" };
    }
    if (isInternalGraphjinURL(src.graphqlUrl)) {
      return {
        denied: true,
        error: "refusing to query the OpenNeko internal GraphJin",
      };
    }

    let token = "openneko-read-only";
    if (src.authMode === "jwt") {
      token = await runGraphjinToken(input.orgId, input.runId);
    }
    const source = {
      id: src.id,
      name: src.name,
      host: hostnameOf(src.graphqlUrl),
    };
    try {
      const { GRAPHJIN_AGENT_MAX_STEPS, askGraphjinAgent, getGraphjinAgentStatus } = await import(
        "../graphjin/agent"
      );
      const agentStatus = await getGraphjinAgentStatus({
        baseUrl: src.graphqlUrl,
        token,
        signal: AbortSignal.timeout(30_000),
      });
      if (!agentStatus.ready) {
        return {
          source,
          agentStatus,
          error: agentStatus.message || "GraphJin agent is not ready",
        };
      }
      if (!agentStatus.read_only) {
        return {
          denied: true,
          source,
          agentStatus,
          error:
            "GraphJin agent is not configured read-only; OpenNeko will not delegate data questions",
        };
      }
      const response = await askGraphjinAgent({
        baseUrl: src.graphqlUrl,
        token,
        signal: AbortSignal.timeout(180_000),
        request: {
          instruction,
          max_steps: Math.min(Math.max(input.maxSteps ?? GRAPHJIN_AGENT_MAX_STEPS, 1), GRAPHJIN_AGENT_MAX_STEPS),
          return_trace: false,
          context: {
            caller: "OpenNeko data agent",
            purpose: "read-only operational data retrieval and analysis",
            constraint:
              "Use catalog and execution evidence. Do not mutate data, configuration, artifacts, tasks, watches, or workflows.",
            ...(await graphjinAgentKnowledge(input.orgId)),
          },
        },
      });
      return { source, agentStatus, response };
    } catch (error) {
      return {
        source,
        error: (error instanceof Error ? error.message : String(error)).slice(
          0,
          1_000,
        ),
      };
    }
  }

  async listRecordCatalog(input: {
    orgId: string;
    runId: string;
    appId?: string;
  }): Promise<RecordsCatalogResult> {
    const [viewer, activeAppIds, runtime] = await Promise.all([
      recordsViewerForRun(input),
      activeRecordAppIds(input.orgId),
      recordsControlPlaneRuntime(),
    ]);
    return runtime.gateway.listCatalog({
      viewer,
      activeAppIds,
      ...(input.appId ? { appId: input.appId } : {}),
    });
  }

  async findRecords(input: {
    orgId: string;
    runId: string;
    appId: string;
    objectApiName: string;
    first?: number;
    after?: string | null;
    sort?: { field: string; direction: "asc" | "desc" };
    search?: string | null;
    filters?: RecordListFilter[];
    myRecords?: boolean;
  }): Promise<RecordsQueryResult> {
    const [viewer, activeAppIds, runtime] = await Promise.all([
      recordsViewerForRun(input),
      activeRecordAppIds(input.orgId),
      recordsControlPlaneRuntime(),
    ]);
    return runtime.gateway.findRecords({ ...input, viewer, activeAppIds });
  }

  async getRecord(input: {
    orgId: string;
    runId: string;
    appId: string;
    objectApiName: string;
    recordId: string;
    allFields?: boolean;
  }): Promise<RecordsDetailResult> {
    const [viewer, activeAppIds, runtime] = await Promise.all([
      recordsViewerForRun(input),
      activeRecordAppIds(input.orgId),
      recordsControlPlaneRuntime(),
    ]);
    return runtime.gateway.getRecord({ ...input, viewer, activeAppIds });
  }

  async findRecycledRecords(input: {
    orgId: string;
    runId: string;
    appId: string;
    objectApiName: string;
    first?: number;
    after?: string | null;
    search?: string | null;
  }): Promise<RecordsRecycleQueryResult> {
    const [viewer, activeAppIds, runtime] = await Promise.all([
      recordsViewerForRun(input),
      activeRecordAppIds(input.orgId),
      recordsControlPlaneRuntime(),
    ]);
    return runtime.gateway.findRecycledRecords({ ...input, viewer, activeAppIds });
  }

  async getRecycledRecord(input: {
    orgId: string;
    runId: string;
    appId: string;
    objectApiName: string;
    recordId: string;
  }): Promise<RecordsRecycleDetailResult> {
    const [viewer, activeAppIds, runtime] = await Promise.all([
      recordsViewerForRun(input),
      activeRecordAppIds(input.orgId),
      recordsControlPlaneRuntime(),
    ]);
    return runtime.gateway.getRecycledRecord({ ...input, viewer, activeAppIds });
  }

  async listRecordBlueprints(input: {
    orgId: string;
    blueprintId?: string;
  }) {
    // orgId deliberately remains part of the brokered call for uniform audit
    // attribution even though shipped blueprint content is organization-neutral.
    void input.orgId;
    const { listRecordAppBlueprints, loadRecordAppBlueprint } = await import(
      "@neko/records"
    );
    const blueprints = input.blueprintId
      ? [await loadRecordAppBlueprint(input.blueprintId)]
      : await listRecordAppBlueprints();
    return {
      blueprints: blueprints.map((blueprint) => ({
        id: blueprint.id,
        version: blueprint.version,
        description: blueprint.description,
        ...(input.blueprintId ? { payload: blueprint.payload } : {}),
      })),
    };
  }

  async saveWorkflowWithTrigger(
    input: SaveWorkflowInput,
  ): Promise<Wire<SaveWorkflowWithTriggerResult>> {
    if (input.createdByRunId) {
      const owner = input.ownerUserId ?? "";
      const existing = (await listWorkflows(input.orgId)).find((w) => w.name === input.name && w.ownerUserId === owner);
      if (existing && !(await runWorkflowFilter(input.orgId, input.createdByRunId))(existing)) {
        throw new Error(`A workflow named "${input.name}" exists and this run cannot change it. Choose another name.`);
      }
    }
    return toWire(await saveWorkflowWithTrigger(input));
  }

  async emitWorkflowOutput(
    input: WorkflowOutputInput,
  ): Promise<Wire<WorkflowOutputRecord>> {
    const output = await emitWorkflowOutput(input);
    notifyWorkflowOutputDeliveryHook(input.orgId, output);
    return toWire(output);
  }

  async listWorkflowsWithTriggers(input: {
    orgId: string;
    limit?: number;
    runId?: string | null;
  }): Promise<{ total: number; workflows: WorkflowListEntry[] }> {
    const visible = await runWorkflowFilter(input.orgId, input.runId);
    const all = (await listWorkflows(input.orgId)).filter(visible);
    const slice = all.slice(0, input.limit ?? 50);
    const triggers = await Promise.all(
      slice.map((w) => listSubscriptionsByWorkflow(input.orgId, w.id)),
    );
    return {
      total: all.length,
      workflows: slice.map((w, i) => {
        const dataTrigger = triggers[i].find(
          (s) => s.sourceKind === "source_change" && s.enabled,
        );
        return { ...toWire(w), when: dataTrigger ? dataTrigger.filter : null };
      }),
    };
  }

  async deleteWorkflow(input: {
    orgId: string;
    workflowId: string;
    runId?: string | null;
  }): Promise<{ found: boolean; name: string | null }> {
    const target = await getWorkflow(input.orgId, input.workflowId);
    if (!target || !(await runWorkflowFilter(input.orgId, input.runId))(target)) {
      return { found: false, name: null };
    }
    const deleted = await deleteWorkflow(input.orgId, input.workflowId);
    return { found: deleted !== null, name: deleted?.name ?? null };
  }

  async upsertActionPolicyByName(
    input: CreateActionPolicyInput,
  ): Promise<Wire<UpsertActionPolicyResult>> {
    return toWire(await upsertActionPolicyByName(input));
  }

  async listActionPolicies(input: {
    orgId: string;
    limit?: number;
  }): Promise<{ total: number; policies: Array<Wire<ActionPolicyRecord>> }> {
    const all = await listAllPolicies(input.orgId);
    return {
      total: all.length,
      policies: all.slice(0, input.limit ?? 50).map((p) => toWire(p)),
    };
  }

  async listPlugins(_input: { orgId: string }): Promise<PluginCatalog> {
    const {
      readManifest,
      createMarketplaceClient,
      OFFICIAL_MARKETPLACE_URL,
      pickInstallVersion,
    } = await import("@open-neko/plugin-install");
    const manifest = await readManifest(process.cwd()).catch(() => null);
    const installed = (manifest?.plugins ?? []).map((p) => ({
      name: p.name,
      version: p.version,
      source: p.installSource ?? "marketplace",
      network: p.permissions?.network ?? [],
      installedAt: p.installedAt ?? null,
    }));
    let available: PluginCatalog["available"] = [];
    let marketplaceError: string | undefined;
    try {
      const marketplace = await createMarketplaceClient().fetch(
        OFFICIAL_MARKETPLACE_URL,
      );
      available = (marketplace.plugins ?? []).flatMap((p) => {
        try {
          return [{ name: p.name, title: p.title || p.name, description: p.description ?? "", version: pickInstallVersion(p).version }];
        } catch {
          return [];
        }
      });
    } catch (err) {
      marketplaceError = err instanceof Error ? err.message : String(err);
    }
    return {
      installed,
      available,
      ...(marketplaceError ? { marketplaceError } : {}),
    };
  }

  async listGroups(input: { orgId: string }) {
    const {
      getGroupGrantsEnabled,
      listDataAccessRules,
      listGroupItemGrants,
      listGroupMembers,
      listIdpGroupRules,
      listUserGroups,
    } = await import("@neko/db");
    const [groups, idpRules, rules, enabled] = await Promise.all([
      listUserGroups(input.orgId),
      listIdpGroupRules(input.orgId),
      listDataAccessRules(input.orgId),
      getGroupGrantsEnabled(input.orgId),
    ]);
    return {
      groups: await Promise.all(
        groups.map(async (group) => ({
          id: group.id,
          slug: group.slug,
          name: group.name,
          kind: group.kind,
          memberCount: group.memberCount,
          members: group.slug === "everyone"
            ? []
            : (await listGroupMembers(input.orgId, group.id)).map((m) => ({ userId: m.userId, email: m.email, sources: m.sources })),
          grants: (await listGroupItemGrants(input.orgId, group.id)).map((g) => ({ itemType: g.itemType, itemId: g.itemId })),
          dataAccess: rules
            .filter((r) => r.groupId === group.id)
            .map((r) => ({ id: r.id, source: r.source, table: r.tableSchema ? `${r.tableSchema}.${r.tableName}` : r.tableName, columns: r.columns, rowFilter: r.rowFilter })),
        })),
      ),
      idpRules: idpRules.map((r) => ({ id: r.id, idpGroupName: r.idpGroupName, ssoGroupId: r.ssoGroupId, userGroupId: r.userGroupId, userGroupName: r.userGroupName })),
      groupDataAccessEnabled: enabled,
    };
  }

  async listUsers(input: { orgId: string }) {
    const {
      administratorUserIds,
      app_user,
      db,
      eq,
      sso_group,
      sso_group_membership,
    } = await import("@neko/db");
    const administrators = await administratorUserIds(input.orgId);
    const rows = await db()
      .select({
        id: app_user.id,
        email: app_user.email,
        name: app_user.name,
        disabledAt: app_user.disabled_at,
        lastLoginAt: app_user.last_login_at,
      })
      .from(app_user)
      .where(eq(app_user.org_id, input.orgId));
    const [groups, memberships] = await Promise.all([
      db()
        .select({
          id: sso_group.id,
          provider: sso_group.provider,
          tenantId: sso_group.tenant_id,
          externalId: sso_group.external_id,
          displayName: sso_group.display_name,
          active: sso_group.active,
        })
        .from(sso_group)
        .where(eq(sso_group.org_id, input.orgId)),
      db()
        .select({
          userId: sso_group_membership.user_id,
          groupId: sso_group_membership.group_id,
        })
        .from(sso_group_membership)
        .where(eq(sso_group_membership.org_id, input.orgId)),
    ]);
    return {
      users: rows.map((r) => ({
        ...r,
        role: administrators.has(r.id) ? "admin" : "member",
        disabledAt: r.disabledAt?.toISOString() ?? null,
        lastLoginAt: r.lastLoginAt?.toISOString() ?? null,
        groupIds: memberships
          .filter((membership) => membership.userId === r.id)
          .map((membership) => membership.groupId),
      })),
      groups,
    };
  }

  async listDataSources(input: { orgId: string }) {
    const { data_source, db, eq } = await import("@neko/db");
    const rows = await db()
      .select()
      .from(data_source)
      .where(eq(data_source.org_id, input.orgId));
    return {
      sources: rows.map((r) => ({
        id: r.id,
        name: r.name,
        label: r.label,
        kind: r.kind,
        authMode: r.auth_mode,
        isDefault: r.is_default,
        enabled: r.enabled,
        host: hostnameOf(r.graphql_url),
        hasMcp: Boolean(r.mcp_url),
      })),
    };
  }

  async describeSourceGraph(input: {
    orgId: string;
    runId?: string | null;
    actorRole?: string | null;
  }) {
    const gate = await requireSourceConfigAccess(input);
    if (!gate.ok) {
      return { reachable: false, denied: true, error: gate.error };
    }

    const { data_source, db, desc, eq } = await import("@neko/db");
    const [src] = await db()
      .select({ name: data_source.name, graphqlUrl: data_source.graphql_url })
      .from(data_source)
      .where(eq(data_source.org_id, input.orgId))
      .orderBy(desc(data_source.is_default), data_source.created_at)
      .limit(1);
    if (!src?.graphqlUrl) {
      return { reachable: false, error: "no data source configured" };
    }
    const { mintGraphjinToken } = await import("../graphjin/token");
    const { graphjinQuery } = await import("../graphjin/client");
    const token = mintGraphjinToken({
      orgId: input.orgId,
      userId: null,
      role: "service",
    });
    const kinds = ["database", "capability", "namespace"] as const;
    type Row = { id: string; name: string; summary: string };
    const buckets: Record<string, Row[]> = {
      database: [],
      capability: [],
      namespace: [],
    };
    try {
      for (const kind of kinds) {
        const res = await graphjinQuery<{ gj_catalog?: Row[] }>({
          baseUrl: src.graphqlUrl,
          query: `query { gj_catalog(where: { kind: { eq: "${kind}" } }, limit: 100) { id name summary } }`,
          headers: { authorization: `Bearer ${token}` },
        });
        if (res.errors?.length) {
          return {
            reachable: false,
            error: res.errors.map((e) => e.message).join("; ").slice(0, 300),
          };
        }
        buckets[kind] = Array.isArray(res.data?.gj_catalog)
          ? res.data.gj_catalog
          : [];
      }
    } catch (err) {
      return {
        reachable: false,
        error: (err instanceof Error ? err.message : String(err)).slice(0, 300),
      };
    }
    return {
      reachable: true,
      sourceName: src.name,
      host: hostnameOf(src.graphqlUrl),
      databases: buckets.database,
      capabilities: buckets.capability,
      namespaces: buckets.namespace,
    };
  }

  async listSourceSecretNames(input: {
    orgId: string;
    runId?: string | null;
    actorRole?: string | null;
  }) {
    const gate = await requireSourceConfigAccess(input);
    if (!gate.ok) {
      return { denied: true, error: gate.error, names: [] };
    }

    const { data_source_secret, db, desc, eq } = await import("@neko/db");
    const rows = await db()
      .select({
        name: data_source_secret.name,
        description: data_source_secret.description,
        updatedAt: data_source_secret.updated_at,
      })
      .from(data_source_secret)
      .where(eq(data_source_secret.org_id, input.orgId))
      .orderBy(desc(data_source_secret.updated_at));
    return {
      names: rows.map((r) => ({
        name: r.name,
        description: r.description,
        updatedAt: r.updatedAt.toISOString(),
      })),
    };
  }

  async importOpenApiSpec(input: {
    orgId: string;
    runId?: string | null;
    actorRole?: string | null;
    url: string;
    baseUrl?: string | null;
  }) {
    const gate = await requireSourceConfigAccess(input);
    if (!gate.ok) return { denied: true, error: gate.error };
    try {
      const { createOpenApiSpecAssetFromUrl } = await import(
        "../graphjin/openapi-assets"
      );
      return {
        asset: await createOpenApiSpecAssetFromUrl({
          orgId: input.orgId,
          createdByUserId: gate.userId,
          url: input.url,
          baseUrl: input.baseUrl,
        }),
      };
    } catch (error) {
      return {
        error: (error instanceof Error ? error.message : String(error)).slice(
          0,
          1_000,
        ),
      };
    }
  }

  async listOpenApiSpecs(input: {
    orgId: string;
    runId?: string | null;
    actorRole?: string | null;
    limit?: number;
  }) {
    const gate = await requireSourceConfigAccess(input);
    if (!gate.ok) {
      return { denied: true, error: gate.error, assets: [] };
    }
    const { listOpenApiSpecAssets } = await import(
      "../graphjin/openapi-assets"
    );
    return {
      assets: await listOpenApiSpecAssets({
        orgId: input.orgId,
        limit: input.limit,
      }),
    };
  }

  async askSourceConfigAgent(input: {
    orgId: string;
    runId?: string | null;
    actorRole?: string | null;
    dataSourceId?: string;
    instruction: string;
    maxSteps?: number;
  }): Promise<SourceConfigAgentResult> {
    const gate = await requireSourceConfigAccess(input);
    if (!gate.ok) {
      return { denied: true, error: gate.error };
    }
    const instruction = input.instruction.trim();
    if (!instruction || instruction.length > 8_000) {
      return {
        error: "instruction must contain between 1 and 8,000 characters",
      };
    }

    const { and, data_source, db, eq } = await import("@neko/db");
    const rows = await db()
      .select({
        id: data_source.id,
        name: data_source.name,
        kind: data_source.kind,
        graphqlUrl: data_source.graphql_url,
      })
      .from(data_source)
      .where(
        and(
          eq(data_source.org_id, input.orgId),
          eq(data_source.enabled, true),
          ...(input.dataSourceId
            ? [eq(data_source.id, input.dataSourceId)]
            : []),
        ),
      )
      .limit(input.dataSourceId ? 1 : 3);
    if (rows.length === 0) {
      return { error: "no enabled customer GraphJin data source was found" };
    }
    if (!input.dataSourceId && rows.length > 1) {
      return {
        error:
          "multiple enabled data sources exist; choose a dataSourceId with list_data_sources before asking the configuration agent",
      };
    }
    const src = rows[0]!;
    if (src.kind !== "graphjin") {
      return { error: "the selected data source is not a GraphJin server" };
    }
    if (isInternalGraphjinURL(src.graphqlUrl)) {
      return {
        denied: true,
        error: "refusing to inspect the OpenNeko internal GraphJin",
      };
    }

    const { askGraphjinAgent, getGraphjinAgentStatus } = await import(
      "../graphjin/agent"
    );
    const { mintGraphjinToken } = await import("../graphjin/token");
    const token = mintGraphjinToken({
      orgId: input.orgId,
      userId: gate.userId,
      role: "admin",
    });
    const timeout = AbortSignal.timeout(90_000);
    try {
      const agentStatus = await getGraphjinAgentStatus({
        baseUrl: src.graphqlUrl,
        token,
        signal: timeout,
      });
      const source = {
        id: src.id,
        name: src.name,
        host: hostnameOf(src.graphqlUrl),
      };
      if (!agentStatus.ready) {
        return {
          source,
          agentStatus,
          error: agentStatus.message || "GraphJin agent is not ready",
        };
      }
      if (!agentStatus.read_only) {
        return {
          denied: true,
          source,
          agentStatus,
          error:
            "GraphJin agent is not configured read-only; OpenNeko will not forward configuration instructions",
        };
      }
      const response = await askGraphjinAgent({
        baseUrl: src.graphqlUrl,
        token,
        signal: timeout,
        request: {
          instruction,
          max_steps: Math.min(Math.max(input.maxSteps ?? 8, 1), 12),
          return_trace: false,
          context: {
            caller: "OpenNeko admin chat",
            purpose: "configuration inspection and planning",
            constraint:
              "Read-only advisory request. Do not apply configuration or schema changes.",
          },
        },
      });
      return { source, agentStatus, response };
    } catch (err) {
      return {
        source: {
          id: src.id,
          name: src.name,
          host: hostnameOf(src.graphqlUrl),
        },
        error: (err instanceof Error ? err.message : String(err)).slice(0, 1_000),
      };
    }
  }

  async previewSourceConfigChange(input: {
    orgId: string;
    runId?: string | null;
    actorRole?: string | null;
    dataSourceId?: string;
    payload: Record<string, unknown>;
  }): Promise<SourceConfigPreviewResult> {
    const gate = await requireSourceConfigAccess(input);
    if (!gate.ok) return { denied: true, error: gate.error };

    const {
      and,
      data_source,
      data_source_secret,
      db,
      eq,
    } = await import("@neko/db");
    const sources = await db()
      .select({
        id: data_source.id,
        name: data_source.name,
        kind: data_source.kind,
        graphqlUrl: data_source.graphql_url,
      })
      .from(data_source)
      .where(
        and(
          eq(data_source.org_id, input.orgId),
          eq(data_source.enabled, true),
          ...(input.dataSourceId
            ? [eq(data_source.id, input.dataSourceId)]
            : []),
        ),
      )
      .limit(input.dataSourceId ? 1 : 3);
    if (sources.length === 0) {
      return { error: "no enabled customer GraphJin data source was found" };
    }
    if (!input.dataSourceId && sources.length > 1) {
      return {
        error:
          "multiple enabled data sources exist; choose a dataSourceId before previewing",
      };
    }
    const src = sources[0]!;
    const source = {
      id: src.id,
      name: src.name,
      host: hostnameOf(src.graphqlUrl),
    };
    if (src.kind !== "graphjin") {
      return { source, error: "the selected data source is not a GraphJin server" };
    }
    if (isInternalGraphjinURL(src.graphqlUrl)) {
      return {
        denied: true,
        source,
        error: "refusing to preview changes against the OpenNeko internal GraphJin",
      };
    }

    let secretValue = "";
    try {
      let specAsset: OpenApiSpecAssetView | undefined;
      const kind = Array.isArray(input.payload.kind)
        ? input.payload.kind[0]
        : input.payload.kind;
      const backend = Array.isArray(input.payload.backend)
        ? input.payload.backend[0]
        : input.payload.backend;
      const sourceName =
        typeof input.payload.name === "string" ? input.payload.name.trim() : "";
      const isManagedLocalFile =
        input.payload.action === "register_source" &&
        kind === "file" &&
        backend === "local";
      let localFileManifest:
        | import("../graphjin/file-source-assets").ManagedFileSourceManifest
        | undefined;
      if (
        input.payload.action === "register_source" &&
        kind === "api"
      ) {
        const specAssetId =
          typeof input.payload.specAssetId === "string"
            ? input.payload.specAssetId
            : "";
        if (!specAssetId) {
          return { source, error: "API source requires an imported OpenAPI specification" };
        }
        const { getOpenApiSpecAsset, openApiSpecAssetView } = await import(
          "../graphjin/openapi-assets"
        );
        const row = await getOpenApiSpecAsset({
          orgId: input.orgId,
          assetId: specAssetId,
        });
        if (!row) {
          return { source, error: "OpenAPI specification is not available" };
        }
        specAsset = openApiSpecAssetView(row);
      }
      if (isManagedLocalFile) {
        const { parseManagedFileSourceManifest } = await import(
          "../graphjin/file-source-assets"
        );
        localFileManifest = parseManagedFileSourceManifest(
          input.payload.localFiles,
          sourceName,
        );
      }
      const {
        assertDatabaseSourcesStayReadOnly,
        buildGraphjinConfigUpdate,
        graphjinConfigPatchHash,
        graphjinInputWithJsonVariables,
      } = await import("../graphjin/config-change");
      const managedSpecsDir = specAsset
        ? "/config/specs"
        : undefined;
      const managedLocalFilesRoot = localFileManifest
        ? (
            await import("../graphjin/file-source")
          ).managedFileSourceRuntimeRoot({
            orgId: input.orgId,
            sourceName,
            runtimeBase:
              process.env.OPENNEKO_GRAPHJIN_FILES_DIR?.trim() || "/config/files",
          })
        : undefined;
      const { update } = await buildGraphjinConfigUpdate(
        input.payload,
        async (name) => {
          const [row] = await db()
            .select({ valueEnc: data_source_secret.value_enc })
            .from(data_source_secret)
            .where(
              and(
                eq(data_source_secret.org_id, input.orgId),
                eq(data_source_secret.name, name),
              ),
            )
            .limit(1);
          if (!row) {
            throw new Error(
              `no stored source secret named "${name}"; add it in GraphJin Config first`,
            );
          }
          const { maybeDecryptSecret } = await import("@neko/secret-crypt");
          secretValue = maybeDecryptSecret(row.valueEnc);
          return secretValue;
        },
        managedSpecsDir || managedLocalFilesRoot
          ? {
              ...(managedSpecsDir
                ? { managedOpenApiSpecsDir: managedSpecsDir }
                : {}),
              ...(managedLocalFilesRoot
                ? { managedLocalFilesRoot }
                : {}),
            }
          : undefined,
      );
      // Hash only the credential-free, typed proposal. A password can be low
      // entropy; even its plain SHA-256 must never enter an approval payload.
      // Canonical JSON survives PostgreSQL JSONB key reordering.
      const patchHash = graphjinConfigPatchHash(input.payload);
      const { graphjinQuery } = await import("../graphjin/client");
      const { mintGraphjinToken } = await import("../graphjin/token");
      const token = mintGraphjinToken({
        orgId: input.orgId,
        userId: gate.userId,
        role: "admin",
        ttlSeconds: 120,
      });
      const headers = { authorization: `Bearer ${token}` };
      const revisionResponse = await graphjinQuery<{
        gj_config?: { catalog_revision?: string; sources?: unknown };
      }>({
        baseUrl: src.graphqlUrl,
        headers,
        query: 'query { gj_config(id: "current") { catalog_revision sources } }',
      });
      const revision = revisionResponse.data?.gj_config?.catalog_revision;
      if (!revision || revisionResponse.errors?.length) {
        return {
          source,
          error:
            revisionResponse.errors?.map((item) => item.message).join("; ") ||
            "GraphJin did not return catalog_revision",
        };
      }
      let currentSources = revisionResponse.data?.gj_config?.sources;
      if (typeof currentSources === "string") {
        try {
          currentSources = JSON.parse(currentSources);
        } catch {
          currentSources = [];
        }
      }
      assertDatabaseSourcesStayReadOnly(update, currentSources);
      if (
        input.payload.action === "register_source" &&
        Array.isArray(currentSources) &&
        currentSources.some(
          (candidate) =>
            candidate &&
            typeof candidate === "object" &&
            !Array.isArray(candidate) &&
            (candidate as Record<string, unknown>).name === sourceName,
        )
      ) {
        return {
          source,
          error: `a GraphJin source named "${sourceName}" already exists`,
        };
      }
      // The managed file is materialized only after approval. Local OpenAPI
      // validation plus the live catalog revision form the proposal preview;
      // the worker performs GraphJin's own two-phase preview after staging it.
      if (specAsset) {
        return {
          source,
          specAsset,
          preview: {
            valid: true,
            patchHash,
            catalogRevision: revision,
            expiresAt: null,
            scope: "managed_openapi",
            reloadMode: "staged_reload",
            changes: {
              action: "register_source",
              source: sourceName,
              kind: "api",
              title: specAsset.title,
              version: specAsset.version,
              baseUrl: specAsset.baseUrl,
              operationCount: specAsset.operationCount,
              authSchemes: specAsset.authSchemes,
              checksumSha256: specAsset.checksumSha256,
            },
            findings: specAsset.warnings,
            errors: null,
          },
        };
      }
      // The worker verifies the approved manifest against the shared volume,
      // then asks GraphJin to preview and apply the staged directory.
      if (localFileManifest) {
        return {
          source,
          preview: {
            valid: true,
            patchHash,
            catalogRevision: revision,
            expiresAt: null,
            scope: "managed_local_files",
            reloadMode: "staged_reload",
            changes: {
              action: "register_source",
              source: sourceName,
              kind: "file",
              backend: "local",
              fileCount: localFileManifest.files.length,
              totalSize: localFileManifest.totalSize,
              readOnly: true,
              access: "authenticated",
            },
            findings: [],
            errors: null,
          },
        };
      }
      const previewInput = graphjinInputWithJsonVariables({
        mode: "preview",
        expected_catalog_revision: revision,
        ...update,
      });
      const response = await graphjinQuery<{
        gj_config?: {
          valid?: boolean;
          preview_id?: string;
          expires_at?: string;
          change_summary_json?: string;
          findings_json?: string;
          errors_json?: string;
        };
      }>({
        baseUrl: src.graphqlUrl,
        headers,
        query: `mutation${previewInput.variableDefinitions} { gj_config(id: "current", update: ${previewInput.literal}) { valid preview_id expires_at change_summary_json findings_json errors_json } }`,
        variables: previewInput.variables,
      });
      if (response.errors?.length) {
        return {
          source,
          error: response.errors.map((item) => item.message).join("; "),
        };
      }
      const row = response.data?.gj_config;
      const scrub = (value: string | undefined): string =>
        secretValue && secretValue.length >= 8
          ? (value ?? "").split(secretValue).join("[REDACTED]")
          : value ?? "";
      return {
        source,
        ...(specAsset ? { specAsset } : {}),
        preview: {
          valid: row?.valid === true && Boolean(row.preview_id),
          patchHash,
          catalogRevision: revision,
          expiresAt: row?.expires_at ?? null,
          scope: null,
          reloadMode: null,
          changes: parseConfigPreviewJSON(scrub(row?.change_summary_json)),
          findings: parseConfigPreviewJSON(scrub(row?.findings_json)),
          errors: parseConfigPreviewJSON(scrub(row?.errors_json)),
        },
      };
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      return {
        source,
        error:
          secretValue && secretValue.length >= 8
            ? message.split(secretValue).join("[REDACTED]").slice(0, 1_000)
            : message.slice(0, 1_000),
      };
    }
  }

  async listAuditTrail(input: {
    orgId: string;
    runId?: string | null;
    limit?: number;
  }) {
    const {
      action_request,
      behavior_alert,
      control_plane_audit,
      and,
      db,
      desc,
      eq,
      gte,
      sql,
      work_run,
    } = await import("@neko/db");
    // Server-side admin gate on the REQUESTING run's K1 actor.
    if (!input.runId) return { denied: true };
    const [run] = await db()
      .select({ role: work_run.actor_role })
      .from(work_run)
      .where(eq(work_run.id, input.runId))
      .limit(1);
    if (run?.role !== "admin") return { denied: true };

    const limit = Math.min(Math.max(input.limit ?? 50, 1), 200);
    const requests = await db()
      .select()
      .from(action_request)
      .where(eq(action_request.org_id, input.orgId))
      .orderBy(desc(action_request.created_at))
      .limit(limit);
    const alerts = await db()
      .select()
      .from(behavior_alert)
      .where(eq(behavior_alert.org_id, input.orgId))
      .orderBy(desc(behavior_alert.created_at))
      .limit(limit);
    const daySince = new Date(Date.now() - 24 * 60 * 60 * 1000);
    const gateway = await db()
      .select({
        runId: control_plane_audit.run_id,
        backend: control_plane_audit.backend,
        actorRole: control_plane_audit.actor_role,
        calls: sql<number>`count(*)::int`,
      })
      .from(control_plane_audit)
      .where(
        and(
          eq(control_plane_audit.org_id, input.orgId),
          gte(control_plane_audit.created_at, daySince),
        ),
      )
      .groupBy(
        control_plane_audit.run_id,
        control_plane_audit.backend,
        control_plane_audit.actor_role,
      )
      .orderBy(sql`count(*) desc`)
      .limit(limit);

    return {
      requests: requests.map((r) => ({
        id: r.id,
        kind: r.kind,
        target: r.target,
        status: r.status,
        scope: r.scope,
        summary: r.summary,
        actorUserId: r.actor_user_id,
        actorRole: r.actor_role,
        actorBackend: r.actor_backend,
        createdAt: r.created_at.toISOString(),
      })),
      alerts: alerts.map((a) => ({
        id: a.id,
        kind: a.kind,
        subject: a.subject,
        observed: a.observed,
        threshold: a.threshold,
        windowSeconds: a.window_seconds,
        acknowledgedAt: a.acknowledged_at?.toISOString() ?? null,
        createdAt: a.created_at.toISOString(),
      })),
      gatewaySummary: gateway,
    };
  }

  async listChannels(input: { orgId: string }) {
    const { channel_identity, channel_workspace, db, eq } = await import(
      "@neko/db"
    );
    const workspaces = await db()
      .select({
        channelPlugin: channel_workspace.channel_plugin,
        workspaceId: channel_workspace.workspace_id,
        createdAt: channel_workspace.created_at,
      })
      .from(channel_workspace)
      .where(eq(channel_workspace.org_id, input.orgId));
    const identities = await db()
      .select({
        id: channel_identity.id,
        channelPlugin: channel_identity.channel_plugin,
        workspaceId: channel_identity.workspace_id,
        channelUserId: channel_identity.channel_user_id,
        displayName: channel_identity.display_name,
        email: channel_identity.email,
        status: channel_identity.status,
        appUserId: channel_identity.app_user_id,
        firstSeenAt: channel_identity.first_seen_at,
        verifiedAt: channel_identity.verified_at,
      })
      .from(channel_identity)
      .where(eq(channel_identity.org_id, input.orgId));
    return {
      workspaces: workspaces.map((w) => ({
        ...w,
        createdAt: w.createdAt?.toISOString() ?? null,
      })),
      identities: identities.map((i) => ({
        ...i,
        firstSeenAt: i.firstSeenAt?.toISOString() ?? null,
        verifiedAt: i.verifiedAt?.toISOString() ?? null,
      })),
    };
  }
}

function hostnameOf(url: string | null): string | null {
  if (!url) return null;
  try {
    return new URL(url).hostname;
  } catch {
    return null;
  }
}

function isInternalGraphjinURL(url: string): boolean {
  try {
    const candidate = new URL(url).host;
    const internal = new URL(
      process.env.OPENNEKO_GRAPHJIN_URL ?? "http://127.0.0.1:8089",
    ).host;
    return candidate === internal;
  } catch {
    return true;
  }
}

function parseConfigPreviewJSON(value: string): unknown {
  if (!value) return null;
  try {
    return JSON.parse(value);
  } catch {
    return value.slice(0, 4_000);
  }
}

export const inProcessControlPlane: AgentControlPlane = new InProcessControlPlane();
