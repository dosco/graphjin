import {
  createServer,
  type IncomingMessage,
  type Server,
  type ServerResponse,
} from "node:http";
import { randomUUID } from "node:crypto";
import { graphjinAgentEnabledForOrg } from "@neko/db";
import type { AgentEvent } from "../agent-backend";
import { inProcessControlPlane, type AgentControlPlane } from "./control-plane";
import { sandboxBrokerHost } from "./sandbox-net";
import {
  traceActionPolicy,
  traceGraphjinQuery,
  traceGraphjinToolCall,
  traceGraphjinToolsList,
  traceLibrarySearch,
  traceMemorySearch,
  traceRecordBlueprints,
  traceWorkflowList,
  unwrapWorkSemanticTraceControlPlane,
} from "./semantic-trace";

/** What a per-run bearer token resolves to — the trust binding. */
export interface RunBinding {
  runId: string;
  orgId: string;
  /** Agent jobs have no work_run actor and intentionally use service reads. */
  kind: "work" | "workflow" | "agent-job";
  /** Present for chat/workflow runs so broker-emitted events can be persisted. */
  threadId?: string;
}

export type AgentBrokerEventSink = (event: AgentEvent) => Promise<void>;

// Next evaluates this module once per route bundle inside one process; the
// broker, its port and its event sinks must be shared, as the warm pool is.
type BrokerState = {
  singleton?: AgentBrokerHandle;
  starting?: Promise<AgentBrokerHandle | undefined>;
  sinks: Map<string, AgentBrokerEventSink>;
};
const brokerHost = globalThis as typeof globalThis & { __opennekoAgentBroker?: BrokerState };
const brokerState: BrokerState = (brokerHost.__opennekoAgentBroker ??= { sinks: new Map() });
const runEventSinks = brokerState.sinks;

/**
 * Attach the host run's normal event sink to MCP bridge emissions. This keeps
 * approval cards, builder confirmations, and other tool-authored events on the
 * same scrubbed/persisted/live-SSE path as backend events.
 */
export function registerAgentBrokerEventSink(
  runId: string,
  sink: AgentBrokerEventSink,
): () => void {
  runEventSinks.set(runId, sink);
  return () => {
    if (runEventSinks.get(runId) === sink) runEventSinks.delete(runId);
  };
}

export interface AgentBrokerDeps {
  /** Host-side control plane (real DB/pg-boss access). */
  controlPlane: AgentControlPlane;
  /** Validate a bearer token → its run binding (undefined = reject). */
  resolveRun(token: string): RunBinding | undefined;
  /** Host-side event sink: scrub + persist. Scrubbing stays here so a
   *  sandboxed agent can't leak a secret it was never given. */
  onEvents(binding: RunBinding, events: AgentEvent[]): Promise<void>;
  /** True when the org reaches GraphJin only through its server-side agent. */
  graphjinAgentOnly?(orgId: string): Promise<boolean>;
}

const DIRECT_GRAPHJIN_ROUTES = new Set(["/v1/graphjin/query", "/v1/graphjin/tools/list", "/v1/graphjin/tools/call"]);

/**
 * Localhost HTTP/JSON broker — the ONLY channel a sandboxed agent turn has
 * back to the trusted control plane. orgId and workRunId are always taken
 * from the token binding, never from the request body, so a compromised
 * sandbox can't act cross-run or cross-org.
 */
export function createAgentBroker(deps: AgentBrokerDeps): Server {
  return createServer((req, res) => {
    void handle(deps, req, res).catch((err) => {
      const message = err instanceof Error ? err.message : String(err);
      // The bridge runs outside the worker process and Hermes captures its
      // stderr inside an ephemeral sandbox. Keep the trusted-side failure in
      // worker logs as well; never include the request body or bearer token.
      console.error(
        `[agent-broker] ${req.method ?? "UNKNOWN"} ${req.url ?? "/"} failed: ${message}`,
      );
      send(res, 500, { error: message });
    });
  });
}

async function handle(
  deps: AgentBrokerDeps,
  req: IncomingMessage,
  res: ServerResponse,
): Promise<void> {
  const auth = req.headers.authorization ?? "";
  const token = auth.startsWith("Bearer ") ? auth.slice(7) : "";
  const binding = deps.resolveRun(token);
  if (!binding) return send(res, 401, { error: "unauthorized" });
  if (req.method !== "POST") return send(res, 405, { error: "method_not_allowed" });

  const body = (await readJson(req)) as Record<string, unknown>;
  const path = (req.url ?? "").split("?")[0];
  // A trusted in-process eval may pass an already-decorated control plane.
  // The broker is the authoritative outer boundary here, so unwrap it before
  // applying the broker trace below and avoid recording every call twice.
  const cp = unwrapWorkSemanticTraceControlPlane(deps.controlPlane);

  // SEC5: every authenticated gateway call is audited with the dual
  // identity (human principal + agent backend). Best-effort — auditing
  // must never fail the call itself.
  void auditControlPlaneCall(binding, path);

  if (DIRECT_GRAPHJIN_ROUTES.has(path) && await (deps.graphjinAgentOnly ?? graphjinAgentEnabledForOrg)(binding.orgId).catch(() => false)) {
    return send(res, 403, { error: "This organization reaches its data through the GraphJin agent." });
  }

  switch (path) {
    case "/v1/policy/evaluate":
      return send(
        res,
        200,
        await traceActionPolicy({
          binding,
          request: {
            scope: body.scope === "internal" ? "internal" : "external",
            kind: String(body.kind ?? ""),
            ...(typeof body.target === "string" || body.target === null
              ? { target: body.target }
              : {}),
            ...(typeof body.riskLevel === "string" || body.riskLevel === null
              ? { riskLevel: body.riskLevel }
              : {}),
          },
          execute: () =>
            cp.evaluateActionPolicy({
              ...body,
              orgId: binding.orgId,
            } as Parameters<AgentControlPlane["evaluateActionPolicy"]>[0]),
        }),
      );
    case "/v1/action/request": {
      // The sandbox's status and policyId are claims. The broker decides both
      // from the rules, so code in the box cannot create an approved request.
      const input = {
        ...body,
        orgId: binding.orgId,
        workRunId: binding.runId,
      } as Parameters<AgentControlPlane["createActionRequest"]>[0];
      const decision = await cp.evaluateActionPolicy({
        orgId: binding.orgId,
        scope: input.scope,
        kind: input.kind,
        target: input.target ?? null,
        payload: input.payload ?? null,
        riskLevel: input.riskLevel ?? null,
      });
      if (decision.decision === "deny" || decision.decision === "no_policy") {
        return send(res, 403, { error: `action ${input.kind} denied: ${decision.reason}` });
      }
      const status =
        input.status === "draft"
          ? "draft"
          : input.status === "approved" && decision.decision === "allow"
            ? "approved"
            : "pending_approval";
      return send(
        res,
        200,
        await cp.createActionRequest({ ...input, policyId: decision.policy.id, status }),
      );
    }
    case "/v1/action/enqueue":
      await cp.enqueueActionExecute({
        orgId: binding.orgId,
        actionRequestId: String(body.actionRequestId),
      });
      return send(res, 200, { ok: true });
    case "/v1/action/wait":
      return send(
        res,
        200,
        await cp.waitForActionExecution({
          orgId: binding.orgId,
          actionRequestId: String(body.actionRequestId),
          ...(typeof body.timeoutMs === "number"
            ? { timeoutMs: body.timeoutMs }
            : {}),
        }),
      );
    case "/v1/memory/remember":
      // CV2: the memory layer is derived server-side from the bound run's
      // actor — an agent-supplied userId is never trusted.
      delete body.userId;
      return send(
        res,
        200,
        await cp.rememberWorkMemory({
          ...body,
          orgId: binding.orgId,
          runId: binding.runId,
        } as Parameters<AgentControlPlane["rememberWorkMemory"]>[0]),
      );
    case "/v1/skills/search":
      // The actor comes from the bound run; an agent-supplied user is never trusted.
      delete body.userId;
      return send(
        res,
        200,
        await cp.searchSkillsForRun({
          orgId: binding.orgId,
          runId: binding.runId,
          query: String(body.query ?? ""),
          ...(typeof body.limit === "number" ? { limit: body.limit } : {}),
        }),
      );
    case "/v1/bot/instructions":
      // The chat comes from the run binding; an agent-supplied thread is never trusted.
      if (!binding.threadId) return send(res, 400, { error: "no chat bound to this run" });
      return send(
        res,
        200,
        await cp.updateBotInstructions({
          orgId: binding.orgId,
          threadId: binding.threadId,
          instructions: String(body.instructions ?? ""),
        }),
      );
    case "/v1/memory/search": {
      delete body.userId;
      const query = String(body.query ?? "");
      const limit = typeof body.limit === "number" ? body.limit : undefined;
      return send(
        res,
        200,
        await traceMemorySearch({
          binding,
          request: { query, ...(limit !== undefined ? { limit } : {}) },
          execute: () =>
            cp.searchWorkMemoryByContext({
              ...body,
              orgId: binding.orgId,
              runId: binding.runId,
            } as Parameters<
              AgentControlPlane["searchWorkMemoryByContext"]
            >[0]),
        }),
      );
    }
    case "/v1/library/search": {
      // Same rule as memory: the personal layer comes from the bound
      // run's owner, never from agent-supplied identity.
      delete body.userId;
      const query = String(body.query ?? "");
      const limit = typeof body.limit === "number" ? body.limit : undefined;
      return send(
        res,
        200,
        await traceLibrarySearch({
          binding,
          request: { query, ...(limit !== undefined ? { limit } : {}) },
          execute: () =>
            cp.searchLibraryForRun({
              ...body,
              orgId: binding.orgId,
              runId: binding.runId,
            } as Parameters<AgentControlPlane["searchLibraryForRun"]>[0]),
        }),
      );
    }
    case "/v1/graphjin/query": {
      const query = String(body.query ?? "");
      const variables =
        body.variables && typeof body.variables === "object"
          ? (body.variables as Record<string, unknown>)
          : undefined;
      const operationName =
        typeof body.operationName === "string" ? body.operationName : undefined;
      return send(
        res,
        200,
        await traceGraphjinQuery({
          binding,
          query,
          ...(variables !== undefined ? { variables } : {}),
          ...(operationName !== undefined ? { operationName } : {}),
          execute: () =>
            cp.queryGraphjinRead({
              query,
              ...(variables !== undefined ? { variables } : {}),
              ...(operationName !== undefined ? { operationName } : {}),
              orgId: binding.orgId,
              ...(binding.kind === "agent-job" ? {} : { runId: binding.runId }),
            }),
        }),
      );
    }
    case "/v1/graphjin/tools/list":
      return send(
        res,
        200,
        await traceGraphjinToolsList({
          binding,
          execute: () =>
            cp.listGraphjinTools({
              orgId: binding.orgId,
              ...(binding.kind === "agent-job" ? {} : { runId: binding.runId }),
            }),
        }),
      );
    case "/v1/graphjin/tools/call": {
      const name = String(body.name ?? "");
      const args =
        body.arguments && typeof body.arguments === "object"
          ? (body.arguments as Record<string, unknown>)
          : undefined;
      return send(
        res,
        200,
        await traceGraphjinToolCall({
          binding,
          toolName: name,
          ...(args !== undefined ? { arguments: args } : {}),
          execute: () =>
            cp.callGraphjinTool({
              orgId: binding.orgId,
              ...(binding.kind === "agent-job" ? {} : { runId: binding.runId }),
              name,
              ...(args !== undefined ? { arguments: args } : {}),
            }),
        }),
      );
    }
    case "/v1/graphjin/agent":
      return send(
        res,
        200,
        await cp.askGraphjinDataAgent({
          orgId: binding.orgId,
          // A job has no work_run row, so it carries no run entitlements.
          ...(binding.kind === "agent-job" ? {} : { runId: binding.runId }),
          instruction: String(body.instruction ?? ""),
          ...(typeof body.dataSourceId === "string"
            ? { dataSourceId: body.dataSourceId }
            : {}),
          ...(typeof body.maxSteps === "number"
            ? { maxSteps: body.maxSteps }
            : {}),
        }),
      );
    case "/v1/records/catalog":
      return send(
        res,
        200,
        await cp.listRecordCatalog({
          orgId: binding.orgId,
          runId: binding.runId,
          ...(typeof body.appId === "string" ? { appId: body.appId } : {}),
        }),
      );
    case "/v1/records/find":
      return send(
        res,
        200,
        await cp.findRecords({
          orgId: binding.orgId,
          runId: binding.runId,
          appId: String(body.appId ?? ""),
          objectApiName: String(body.objectApiName ?? ""),
          ...(typeof body.first === "number" ? { first: body.first } : {}),
          ...(typeof body.after === "string" ? { after: body.after } : {}),
          ...(typeof body.search === "string" ? { search: body.search } : {}),
          ...(Array.isArray(body.filters)
            ? {
                filters: body.filters as Parameters<
                  AgentControlPlane["findRecords"]
                >[0]["filters"],
              }
            : {}),
          ...(body.sort && typeof body.sort === "object"
            ? {
                sort: body.sort as NonNullable<
                  Parameters<AgentControlPlane["findRecords"]>[0]["sort"]
                >,
              }
            : {}),
          ...(typeof body.myRecords === "boolean"
            ? { myRecords: body.myRecords }
            : {}),
        }),
      );
    case "/v1/records/get":
      return send(
        res,
        200,
        await cp.getRecord({
          orgId: binding.orgId,
          runId: binding.runId,
          appId: String(body.appId ?? ""),
          objectApiName: String(body.objectApiName ?? ""),
          recordId: String(body.recordId ?? ""),
          ...(typeof body.allFields === "boolean"
            ? { allFields: body.allFields }
            : {}),
        }),
      );
    case "/v1/records/recycle/find":
      return send(
        res,
        200,
        await cp.findRecycledRecords({
          orgId: binding.orgId,
          runId: binding.runId,
          appId: String(body.appId ?? ""),
          objectApiName: String(body.objectApiName ?? ""),
          ...(typeof body.first === "number" ? { first: body.first } : {}),
          ...(typeof body.after === "string" ? { after: body.after } : {}),
          ...(typeof body.search === "string" ? { search: body.search } : {}),
        }),
      );
    case "/v1/records/recycle/get":
      return send(
        res,
        200,
        await cp.getRecycledRecord({
          orgId: binding.orgId,
          runId: binding.runId,
          appId: String(body.appId ?? ""),
          objectApiName: String(body.objectApiName ?? ""),
          recordId: String(body.recordId ?? ""),
        }),
      );
    case "/v1/records/blueprints": {
      const blueprintId =
        typeof body.blueprintId === "string" ? body.blueprintId : undefined;
      return send(
        res,
        200,
        await traceRecordBlueprints({
          binding,
          request: {
            ...(blueprintId !== undefined ? { blueprintId } : {}),
          },
          execute: () =>
            cp.listRecordBlueprints({
              orgId: binding.orgId,
              ...(blueprintId !== undefined ? { blueprintId } : {}),
            }),
        }),
      );
    }
    case "/v1/workflow/save":
      return send(
        res,
        200,
        await cp.saveWorkflowWithTrigger({
          ...body,
          orgId: binding.orgId,
          createdByRunId: binding.runId,
        } as Parameters<AgentControlPlane["saveWorkflowWithTrigger"]>[0]),
      );
    case "/v1/workflow-output/emit":
      return send(
        res,
        200,
        await cp.emitWorkflowOutput(workflowOutputInputFromBody(body, binding)),
      );
    case "/v1/workflow/list": {
      const limit = typeof body.limit === "number" ? body.limit : undefined;
      return send(
        res,
        200,
        await traceWorkflowList({
          binding,
          request: { ...(limit !== undefined ? { limit } : {}) },
          execute: () =>
            cp.listWorkflowsWithTriggers({
              orgId: binding.orgId,
              limit,
              runId: binding.runId,
            }),
        }),
      );
    }
    case "/v1/workflow/delete":
      // orgId comes from the token binding, never the body — a sandbox
      // can't delete another org's workflow by passing its id.
      return send(
        res,
        200,
        await cp.deleteWorkflow({
          orgId: binding.orgId,
          workflowId: String(body.workflowId),
          runId: binding.runId,
        }),
      );
    case "/v1/rule/save":
      return send(
        res,
        200,
        await cp.upsertActionPolicyByName({
          ...body,
          orgId: binding.orgId,
          createdByRunId: binding.runId,
        } as Parameters<AgentControlPlane["upsertActionPolicyByName"]>[0]),
      );
    case "/v1/rule/list":
      return send(
        res,
        200,
        await cp.listActionPolicies({
          orgId: binding.orgId,
          limit: typeof body.limit === "number" ? body.limit : undefined,
        }),
      );
    case "/v1/plugins/list":
      return send(res, 200, await cp.listPlugins({ orgId: binding.orgId }));
    case "/v1/users/list":
      return send(res, 200, await cp.listUsers({ orgId: binding.orgId }));
    case "/v1/groups/list":
      return send(res, 200, await cp.listGroups({ orgId: binding.orgId }));
    case "/v1/channels/list":
      return send(res, 200, await cp.listChannels({ orgId: binding.orgId }));
    case "/v1/datasources/list":
      return send(res, 200, await cp.listDataSources({ orgId: binding.orgId }));
    case "/v1/source-graph/describe":
      return send(
        res,
        200,
        await cp.describeSourceGraph({
          orgId: binding.orgId,
          runId: binding.runId,
        }),
      );
    case "/v1/source-secrets/names":
      return send(
        res,
        200,
        await cp.listSourceSecretNames({
          orgId: binding.orgId,
          runId: binding.runId,
        }),
      );
    case "/v1/openapi/import":
      return send(
        res,
        200,
        await cp.importOpenApiSpec({
          orgId: binding.orgId,
          runId: binding.runId,
          url: String(body.url ?? ""),
          baseUrl: typeof body.baseUrl === "string" ? body.baseUrl : null,
        }),
      );
    case "/v1/openapi/list":
      return send(
        res,
        200,
        await cp.listOpenApiSpecs({
          orgId: binding.orgId,
          runId: binding.runId,
          limit: typeof body.limit === "number" ? body.limit : undefined,
        }),
      );
    case "/v1/source-config/agent":
      return send(
        res,
        200,
        await cp.askSourceConfigAgent({
          orgId: binding.orgId,
          runId: binding.runId,
          dataSourceId:
            typeof body.dataSourceId === "string"
              ? body.dataSourceId
              : undefined,
          instruction: String(body.instruction ?? ""),
          maxSteps:
            typeof body.maxSteps === "number" ? body.maxSteps : undefined,
        }),
      );
    case "/v1/source-config/preview":
      return send(
        res,
        200,
        await cp.previewSourceConfigChange({
          orgId: binding.orgId,
          runId: binding.runId,
          dataSourceId:
            typeof body.dataSourceId === "string"
              ? body.dataSourceId
              : undefined,
          payload:
            body.payload && typeof body.payload === "object"
              ? (body.payload as Record<string, unknown>)
              : {},
        }),
      );
    case "/v1/audit/list":
      // ADM4: the admin gate runs on the BOUND run's actor — the
      // sandbox can't claim someone else's run.
      return send(
        res,
        200,
        await cp.listAuditTrail({
          orgId: binding.orgId,
          runId: binding.runId,
          limit: typeof body.limit === "number" ? body.limit : undefined,
        }),
      );
    case "/v1/events":
      await deps.onEvents(binding, (body.events as AgentEvent[]) ?? []);
      return send(res, 200, { ok: true });
    default:
      return send(res, 404, { error: "not_found" });
  }
}

// Per-run dual-identity cache so auditing costs one DB lookup per run,
// not per call. Bounded; runs are short-lived.
const runIdentityCache = new Map<
  string,
  { userId: string | null; role: string | null; backend: string | null }
>();
const RUN_IDENTITY_CACHE_MAX = 500;

async function auditControlPlaneCall(
  binding: RunBinding,
  path: string,
): Promise<void> {
  try {
    const { control_plane_audit, db, eq, work_run } = await import("@neko/db");
    let identity = runIdentityCache.get(binding.runId);
    if (!identity) {
      const [run] = await db()
        .select({
          userId: work_run.actor_user_id,
          role: work_run.actor_role,
          backend: work_run.backend,
        })
        .from(work_run)
        .where(eq(work_run.id, binding.runId))
        .limit(1);
      identity = run ?? { userId: null, role: null, backend: null };
      if (runIdentityCache.size >= RUN_IDENTITY_CACHE_MAX) {
        runIdentityCache.clear();
      }
      runIdentityCache.set(binding.runId, identity);
    }
    await db().insert(control_plane_audit).values({
      org_id: binding.orgId,
      run_id: binding.runId,
      path,
      actor_user_id: identity.userId,
      actor_role: identity.role,
      backend: identity.backend,
    });
  } catch (err) {
    console.warn(
      `[agent-broker] audit insert failed (call proceeded): ${err instanceof Error ? err.message : err}`,
    );
  }
}

function readJson(req: IncomingMessage): Promise<unknown> {
  return new Promise((resolve, reject) => {
    let data = "";
    req.on("data", (c: Buffer) => {
      data += c.toString("utf8");
      if (data.length > 8_000_000) req.destroy();
    });
    req.on("end", () => {
      try {
        resolve(data ? JSON.parse(data) : {});
      } catch (err) {
        reject(err);
      }
    });
    req.on("error", reject);
  });
}

function send(res: ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(JSON.stringify(body));
}

function workflowOutputInputFromBody(
  body: Record<string, unknown>,
  binding: RunBinding,
): Parameters<AgentControlPlane["emitWorkflowOutput"]>[0] {
  return {
    ...body,
    orgId: binding.orgId,
    workRunId: binding.runId,
    timeWindowStart: optionalDate(body.timeWindowStart, "timeWindowStart"),
    timeWindowEnd: optionalDate(body.timeWindowEnd, "timeWindowEnd"),
  } as Parameters<AgentControlPlane["emitWorkflowOutput"]>[0];
}

function optionalDate(value: unknown, field: string): Date | null | undefined {
  if (value === undefined) return undefined;
  if (value === null) return null;
  if (value instanceof Date) return value;
  if (typeof value !== "string" && typeof value !== "number") {
    throw new Error(`${field} must be an ISO date string or null`);
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    throw new Error(`${field} must be a valid date`);
  }
  return date;
}

/** A running broker + its per-run token registry. */
export interface AgentBrokerHandle {
  /** URL the sandboxed agent reaches the broker at. */
  readonly url: string;
  /** Actual listening port (resolved when port 0 / ephemeral is requested). */
  readonly port: number;
  /** Mint-or-reuse a per-run bearer token; pass the return into the sandbox. */
  tokenFor(binding: RunBinding): string;
  /** Drop a finished run's token. */
  release(runId: string): void;
  close(): Promise<void>;
}

export interface StartAgentBrokerOptions {
  controlPlane: AgentControlPlane;
  /** Host-side scrub + persist of events posted to /v1/events. Default no-op
   *  (agent events normally stream over the launcher's stdout channel). */
  onEvents?: (binding: RunBinding, events: AgentEvent[]) => Promise<void>;
  /** Host or address the sandbox uses to reach this broker. Packaged Compose
   *  uses its private container IP; host mode defaults to the gateway alias. */
  hostAlias?: string;
  /** Port to listen on. It is exposed only within the packaged Compose
   *  network; host-mode setups may publish it explicitly. */
  port: number;
}

/**
 * Start a long-lived broker bound to a host control plane, with an in-memory
 * per-run token registry. The worker/web start ONE per process; each run mints
 * a token via {@link AgentBrokerHandle.tokenFor} and releases it on completion.
 */
export async function startAgentBroker(
  opts: StartAgentBrokerOptions,
): Promise<AgentBrokerHandle> {
  const tokens = new Map<string, RunBinding>(); // token -> binding
  const byRun = new Map<string, string>(); // runId -> token

  const server = createAgentBroker({
    controlPlane: opts.controlPlane,
    resolveRun: (token) => tokens.get(token),
    onEvents: opts.onEvents ?? (async () => {}),
  });

  await new Promise<void>((resolve, reject) => {
    const onErr = (e: Error) => reject(e);
    server.once("error", onErr);
    server.listen(opts.port, "0.0.0.0", () => {
      server.removeListener("error", onErr);
      resolve();
    });
  });

  const addr = server.address();
  const port = typeof addr === "object" && addr ? addr.port : opts.port;
  const host = opts.hostAlias ?? "host.openshell.internal";

  return {
    url: `http://${host}:${port}`,
    port,
    tokenFor(binding) {
      const existing = byRun.get(binding.runId);
      if (existing) return existing;
      const token = randomUUID();
      tokens.set(token, binding);
      byRun.set(binding.runId, token);
      return token;
    },
    release(runId) {
      const token = byRun.get(runId);
      if (token) {
        tokens.delete(token);
        byRun.delete(runId);
      }
    },
    async close() {
      await new Promise<void>((resolve) => server.close(() => resolve()));
    },
  };
}


/**
 * Lazily start the per-process agent broker, bound to the in-process control
 * plane. SEC9: OpenShell is the only agent runtime, so every control-plane
 * host runs a broker. Packaged sandboxes dial the container's private Compose
 * IP; host-mode setups use host.openshell.internal.
 * Idempotent — one broker per process, shared by runWorkRun (channel runs)
 * and the web chat route.
 */
export function ensureAgentBroker(): Promise<AgentBrokerHandle | undefined> {
  if (brokerState.singleton) return Promise.resolve(brokerState.singleton);
  if (!brokerState.starting) {
    const pinned = Number(process.env.OPENNEKO_BROKER_PORT) || 0;
    brokerState.starting = startAgentBroker({
      controlPlane: inProcessControlPlane,
      hostAlias: sandboxBrokerHost(),
      port: pinned || 4199,
      onEvents: routeBrokerEvents,
    })
      .catch((e: NodeJS.ErrnoException) => {
        // Unpinned default only: web + worker on one host (dev) both reach
        // for 4199 — fall back to an ephemeral port; sandboxes get the real
        // port via the handle URL. A pinned port must fail loudly because the
        // sandbox policy and Compose exposure both expect that exact port.
        if (pinned || e.code !== "EADDRINUSE") throw e;
        return startAgentBroker({
          controlPlane: inProcessControlPlane,
          hostAlias: sandboxBrokerHost(),
          port: 0,
          onEvents: routeBrokerEvents,
        });
      })
      .then((h) => {
        brokerState.singleton = h;
        return h;
      })
      .catch((e) => {
        brokerState.starting = undefined; // allow a retry on the next run
        throw e;
      });
  }
  return brokerState.starting;
}

/**
 * Stop and forget the process-wide broker.
 *
 * Long-lived web/worker hosts normally keep it for their full lifetime. Short-
 * lived hosts such as the eval CLI must call this during teardown so the
 * broker's listening socket does not keep the Node process alive.
 */
export async function shutdownAgentBroker(): Promise<void> {
  const starting = brokerState.starting;
  const active =
    brokerState.singleton ??
    (starting ? await starting.catch(() => undefined) : undefined);
  brokerState.singleton = undefined;
  brokerState.starting = undefined;
  await active?.close();
}

async function routeBrokerEvents(
  binding: RunBinding,
  events: AgentEvent[],
): Promise<void> {
  const sink = runEventSinks.get(binding.runId);
  if (sink) {
    for (const event of events) await sink(event);
    return;
  }

  // Job callers should register their scrubbed sink. Keep a durable fallback
  // for short-lived hosts or a process race so a successfully-created action
  // request never loses its inline approval event.
  if (!binding.threadId) {
    console.warn(
      `[agent-broker] dropped ${events.length} event(s) for ${binding.runId}: no event sink or thread binding`,
    );
    return;
  }
  const { appendWorkRunEvent } = await import("./store");
  for (const event of events) {
    await appendWorkRunEvent({
      orgId: binding.orgId,
      threadId: binding.threadId,
      runId: binding.runId,
      event,
    });
  }
}
