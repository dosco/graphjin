import { createHash } from "node:crypto";
import type { AgentControlPlane } from "./control-plane";

export const WORK_SEMANTIC_TRACE_SCHEMA_VERSION =
  "openneko.work.semantic-trace/v1" as const;

export type WorkSemanticTraceStatus = "ok" | "error";
export type WorkSemanticTraceSource = "trusted-broker" | "trusted-host";

export type WorkSemanticTraceBinding = {
  runId: string;
  orgId: string;
  kind: "work" | "workflow" | "agent-job";
};

type WorkSemanticTraceBase = {
  schemaVersion: typeof WORK_SEMANTIC_TRACE_SCHEMA_VERSION;
  /** Invocation order within the bound run. */
  sequence: number;
  timestamp: string;
  runId: string;
  orgId: string;
  runKind: WorkSemanticTraceBinding["kind"];
  source: WorkSemanticTraceSource;
  status: WorkSemanticTraceStatus;
  durationMs: number;
  /** Digest of the model-supplied portion of the request; never the raw body. */
  requestDigest: string;
  /** Digest of the control-plane result; absent when execution threw. */
  responseDigest?: string;
  /** Error class or a content-free logical error category. */
  errorType?: string;
  /** Digest of the error message/result; the text itself is never retained. */
  errorDigest?: string;
};

export type WorkSemanticMemoryEvidence = {
  id: string;
  contentDigest: string;
  layer: "team" | "personal";
  scope?: string;
  kind?: string;
};

export type WorkSemanticTraceEvent =
  | (WorkSemanticTraceBase & {
      operation: "memory.prefetched";
      evidence: {
        returnedCount: number;
        memories: WorkSemanticMemoryEvidence[];
      };
    })
  | (WorkSemanticTraceBase & {
      operation: "memory.search";
      evidence: {
        requestedLimit?: number;
        returnedCount: number;
        memories: WorkSemanticMemoryEvidence[];
      };
    })
  | (WorkSemanticTraceBase & {
      operation: "skill.loaded";
      evidence: {
        id: string;
        contentDigest: string;
        sourceDigest?: string;
      };
    })
  | (WorkSemanticTraceBase & {
      operation: "library.search";
      evidence: {
        requestedLimit?: number;
        returnedCount: number;
        concepts: Array<{
          id: string;
          bodyDigest: string;
          layer: "team" | "personal";
          status?: string;
          sourceDocumentId?: string;
          sourceDigests: string[];
        }>;
      };
    })
  | (WorkSemanticTraceBase & {
      operation: "workflow.list";
      evidence: {
        requestedLimit?: number;
        total?: number;
        returnedCount: number;
        workflows: Array<{
          id: string;
          definitionDigest: string;
          enabled?: boolean;
          status?: string;
        }>;
      };
    })
  | (WorkSemanticTraceBase & {
      operation: "action.policy";
      evidence: {
        kind: string;
        scope: "external" | "internal";
        decision: "allow" | "needs_approval" | "deny" | "no_policy";
        mode?: string;
        targetDigest?: string;
        policyDigest?: string;
      };
    })
  | (WorkSemanticTraceBase & {
      operation: "graphjin.tools_list";
      evidence: {
        returnedCount: number;
        catalogDigest?: string;
        tools: Array<{ name: string; schemaDigest: string }>;
      };
    })
  | (WorkSemanticTraceBase & {
      operation: "graphjin.catalog" | "graphjin.call";
      evidence: {
        toolName: string;
        argumentsDigest: string;
      };
    })
  | (WorkSemanticTraceBase & {
      operation: "graphjin.execute";
      evidence: {
        toolName: string;
        queryDigest: string;
        variablesDigest?: string;
        operationType: "query" | "mutation" | "subscription" | "unknown";
      };
    });

export type WorkSemanticTraceSink = (
  event: WorkSemanticTraceEvent,
) => Promise<void> | void;

type RegisteredSink = {
  sink: WorkSemanticTraceSink;
  nextSequence: number;
};

const runSinks = new Map<string, RegisteredSink>();

/**
 * Install one private semantic-evidence sink for a run. The sink is deliberately
 * separate from user-visible AgentEvents and metadata-only telemetry: it is fed
 * only after the trusted broker or decorated host has invoked the control plane.
 */
export function registerWorkSemanticTraceSink(
  runId: string,
  sink: WorkSemanticTraceSink,
): () => void {
  const registration = { sink, nextSequence: 1 };
  runSinks.set(runId, registration);
  return () => {
    if (runSinks.get(runId) === registration) runSinks.delete(runId);
  };
}

/** Stable, content-safe digest for requests, results, and fixture definitions. */
export function workSemanticDigest(value: unknown): string {
  return `sha256:${createHash("sha256").update(canonicalJson(value)).digest("hex")}`;
}

type TraceOperation = WorkSemanticTraceEvent["operation"];

type TraceContext = {
  binding: WorkSemanticTraceBinding;
  registration: RegisteredSink;
  sequence: number;
  startedAt: number;
  operation: TraceOperation;
  source: WorkSemanticTraceSource;
  requestDigest: string;
};

function beginTrace(
  binding: WorkSemanticTraceBinding,
  operation: TraceOperation,
  request: unknown,
  source: WorkSemanticTraceSource = "trusted-broker",
): TraceContext | null {
  const registration = runSinks.get(binding.runId);
  if (!registration) return null;
  const sequence = registration.nextSequence;
  registration.nextSequence += 1;
  return {
    binding,
    registration,
    sequence,
    startedAt: Date.now(),
    operation,
    source,
    requestDigest: workSemanticDigest(request),
  };
}

async function deliver(
  context: TraceContext | null,
  buildEvent: () => WorkSemanticTraceEvent,
): Promise<void> {
  if (!context) return;
  try {
    await context.registration.sink(buildEvent());
  } catch {
    // Evidence collection must never change the product call's result. Eval
    // drivers detect a missing event and classify the episode as unscorable.
    console.warn(
      `[work-semantic-trace] emit failed run=${context.binding.runId} operation=${context.operation}`,
    );
  }
}

function baseEvent(
  context: TraceContext,
  status: WorkSemanticTraceStatus,
  result?: unknown,
  error?: unknown,
): WorkSemanticTraceBase {
  return {
    schemaVersion: WORK_SEMANTIC_TRACE_SCHEMA_VERSION,
    sequence: context.sequence,
    timestamp: new Date(context.startedAt).toISOString(),
    runId: context.binding.runId,
    orgId: context.binding.orgId,
    runKind: context.binding.kind,
    source: context.source,
    status,
    durationMs: Math.max(0, Date.now() - context.startedAt),
    requestDigest: context.requestDigest,
    ...(result !== undefined
      ? { responseDigest: workSemanticDigest(result) }
      : {}),
    ...(error !== undefined
      ? {
          errorType: safeErrorType(error),
          errorDigest: workSemanticDigest(
            error instanceof Error ? error.message : error,
          ),
        }
      : {}),
  };
}

function safeErrorType(error: unknown): string {
  if (error instanceof TypeError) return "TypeError";
  if (error instanceof RangeError) return "RangeError";
  if (error instanceof SyntaxError) return "SyntaxError";
  if (error instanceof URIError) return "URIError";
  if (error instanceof EvalError) return "EvalError";
  return error instanceof Error ? "Error" : "control_plane_error";
}

async function traced<T>(input: {
  binding: WorkSemanticTraceBinding;
  operation: TraceOperation;
  source?: WorkSemanticTraceSource;
  request: unknown;
  execute: () => Promise<T>;
  logicalError?: (result: T) => boolean;
  event: (
    base: WorkSemanticTraceBase,
    result: T | undefined,
  ) => WorkSemanticTraceEvent;
}): Promise<T> {
  const context = beginTrace(
    input.binding,
    input.operation,
    input.request,
    input.source,
  );
  let result: T;
  try {
    result = await input.execute();
  } catch (error) {
    if (context) {
      await deliver(context, () =>
        input.event(baseEvent(context, "error", undefined, error), undefined),
      );
    }
    throw error;
  }
  if (context) {
    await deliver(context, () => {
      const logicalError = input.logicalError?.(result) ?? false;
      const base = baseEvent(context, logicalError ? "error" : "ok", result);
      if (logicalError) {
        base.errorType = "tool_result_error";
        base.errorDigest = workSemanticDigest(result);
      }
      return input.event(base, result);
    });
  }
  return result;
}

export function traceMemorySearch<T>(input: {
  binding: WorkSemanticTraceBinding;
  source?: WorkSemanticTraceSource;
  request: { query: string; limit?: number };
  execute: () => Promise<T>;
}): Promise<T> {
  return traced({
    ...input,
    operation: "memory.search",
    event: (base, result) => {
      const rows = asArray(result);
      return {
        ...base,
        operation: "memory.search",
        evidence: {
          ...(input.request.limit !== undefined
            ? { requestedLimit: input.request.limit }
            : {}),
          returnedCount: rows.length,
          memories: rows.flatMap((row) => {
            const memory = record(record(row)?.memory);
            const id = string(memory?.id);
            if (!memory || !id) return [];
            const scope = string(memory.scope);
            const kind = string(memory.kind);
            return [
              {
                id,
                contentDigest: workSemanticDigest(memory.text ?? null),
                layer: memory.userId ? ("personal" as const) : ("team" as const),
                ...(scope ? { scope } : {}),
                ...(kind ? { kind } : {}),
              },
            ];
          }),
        },
      };
    },
  });
}

export function traceLibrarySearch<T>(input: {
  binding: WorkSemanticTraceBinding;
  source?: WorkSemanticTraceSource;
  request: { query: string; limit?: number };
  execute: () => Promise<T>;
}): Promise<T> {
  return traced({
    ...input,
    operation: "library.search",
    event: (base, result) => {
      const rows = asArray(result);
      return {
        ...base,
        operation: "library.search",
        evidence: {
          ...(input.request.limit !== undefined
            ? { requestedLimit: input.request.limit }
            : {}),
          returnedCount: rows.length,
          concepts: rows.flatMap((row) => {
            const resultRow = record(row);
            const concept = record(resultRow?.concept);
            const id = string(concept?.id);
            if (!concept || !id) return [];
            const explicitLayer = string(resultRow?.layer);
            const sources = asArray(concept.sources);
            const status = string(concept.status);
            const sourceDocumentId = string(concept.sourceDocumentId);
            return [
              {
                id,
                bodyDigest: workSemanticDigest(concept.body ?? null),
                layer:
                  explicitLayer === "personal" || concept.userId
                    ? ("personal" as const)
                    : ("team" as const),
                ...(status ? { status } : {}),
                ...(sourceDocumentId ? { sourceDocumentId } : {}),
                sourceDigests: sources.map((source) =>
                  workSemanticDigest(record(source)?.resource ?? source),
                ),
              },
            ];
          }),
        },
      };
    },
  });
}

export function traceWorkflowList<T>(input: {
  binding: WorkSemanticTraceBinding;
  source?: WorkSemanticTraceSource;
  request: { limit?: number };
  execute: () => Promise<T>;
}): Promise<T> {
  return traced({
    ...input,
    operation: "workflow.list",
    event: (base, result) => {
      const resultRecord = record(result);
      const workflows = asArray(resultRecord?.workflows);
      const total = number(resultRecord?.total);
      return {
        ...base,
        operation: "workflow.list",
        evidence: {
          ...(input.request.limit !== undefined
            ? { requestedLimit: input.request.limit }
            : {}),
          ...(total !== undefined ? { total } : {}),
          returnedCount: workflows.length,
          workflows: workflows.flatMap((workflow) => {
            const value = record(workflow);
            const id = string(value?.id);
            if (!value || !id) return [];
            const status = string(value.status);
            return [
              {
                id,
                definitionDigest: workSemanticDigest(value),
                ...(typeof value.enabled === "boolean"
                  ? { enabled: value.enabled }
                  : {}),
                ...(status ? { status } : {}),
              },
            ];
          }),
        },
      };
    },
  });
}

export function traceActionPolicy<
  T extends {
    decision: "allow" | "needs_approval" | "deny" | "no_policy";
    mode?: string;
    policy?: { id?: string; name?: string };
  },
>(input: {
  binding: WorkSemanticTraceBinding;
  source?: WorkSemanticTraceSource;
  request: {
    scope: "external" | "internal";
    kind: string;
    target?: string | null;
    riskLevel?: string | null;
  };
  execute: () => Promise<T>;
}): Promise<T> {
  return traced({
    binding: input.binding,
    ...(input.source !== undefined ? { source: input.source } : {}),
    operation: "action.policy",
    request: input.request,
    execute: input.execute,
    event: (base, result) => ({
      ...base,
      operation: "action.policy",
      evidence: {
        kind: input.request.kind,
        scope: input.request.scope,
        decision: result?.decision ?? "no_policy",
        ...(result?.mode ? { mode: result.mode } : {}),
        ...(input.request.target
          ? { targetDigest: workSemanticDigest(input.request.target) }
          : {}),
        ...(result?.policy
          ? { policyDigest: workSemanticDigest(result.policy.id ?? result.policy.name) }
          : {}),
      },
    }),
  });
}

export function traceGraphjinToolsList<T>(input: {
  binding: WorkSemanticTraceBinding;
  source?: WorkSemanticTraceSource;
  execute: () => Promise<T>;
}): Promise<T> {
  return traced({
    ...input,
    operation: "graphjin.tools_list",
    request: {},
    event: (base, result) => {
      const tools = asArray(result).flatMap((tool) => {
        const value = record(tool);
        const name = string(value?.name);
        if (!value || !name) return [];
        return [
          {
            name,
            schemaDigest: workSemanticDigest(value.inputSchema ?? null),
          },
        ];
      });
      return {
        ...base,
        operation: "graphjin.tools_list",
        evidence: {
          returnedCount: tools.length,
          ...(result !== undefined
            ? { catalogDigest: workSemanticDigest(result) }
            : {}),
          tools,
        },
      };
    },
  });
}

export function traceGraphjinToolCall<T>(input: {
  binding: WorkSemanticTraceBinding;
  source?: WorkSemanticTraceSource;
  toolName: string;
  arguments?: Record<string, unknown>;
  execute: () => Promise<T>;
}): Promise<T> {
  const args = input.arguments ?? {};
  const operation = graphjinOperation(input.toolName);
  return traced({
    binding: input.binding,
    ...(input.source !== undefined ? { source: input.source } : {}),
    operation,
    request: { name: input.toolName, arguments: args },
    execute: input.execute,
    logicalError: isToolResultError,
    event: (base) => {
      if (operation === "graphjin.execute") {
        const query = string(args.query) ?? "";
        return {
          ...base,
          operation,
          evidence: {
            toolName: input.toolName,
            queryDigest: workSemanticDigest(query),
            ...(args.variables !== undefined
              ? { variablesDigest: workSemanticDigest(args.variables) }
              : {}),
            operationType: graphqlOperationType(query),
          },
        };
      }
      return {
        ...base,
        operation,
        evidence: {
          toolName: input.toolName,
          argumentsDigest: workSemanticDigest(args),
        },
      };
    },
  });
}

export function traceGraphjinQuery<T>(input: {
  binding: WorkSemanticTraceBinding;
  source?: WorkSemanticTraceSource;
  query: string;
  variables?: Record<string, unknown>;
  operationName?: string;
  execute: () => Promise<T>;
}): Promise<T> {
  return traced({
    binding: input.binding,
    ...(input.source !== undefined ? { source: input.source } : {}),
    operation: "graphjin.execute",
    request: {
      query: input.query,
      variables: input.variables,
      operationName: input.operationName,
    },
    execute: input.execute,
    logicalError: isGraphqlResultError,
    event: (base) => ({
      ...base,
      operation: "graphjin.execute",
      evidence: {
        toolName: "queryGraphjinRead",
        queryDigest: workSemanticDigest(input.query),
        ...(input.variables !== undefined
          ? { variablesDigest: workSemanticDigest(input.variables) }
          : {}),
        operationType: graphqlOperationType(input.query),
      },
    }),
  });
}

export type WorkSemanticHostEventInput =
  | {
      binding: WorkSemanticTraceBinding;
      operation: "memory.prefetched";
      memories: readonly WorkSemanticMemoryEvidence[];
    }
  | {
      binding: WorkSemanticTraceBinding;
      operation: "skill.loaded";
      skill: {
        id: string;
        contentDigest: string;
        sourceDigest?: string;
      };
    };

/**
 * Record host-only context acquisition that never crosses the broker. Callers
 * provide stable resource identifiers and precomputed sha256 digests only;
 * invalid digest fields cause the evidence emission to be dropped, not leaked.
 */
export async function recordWorkSemanticHostEvent(
  input: WorkSemanticHostEventInput,
): Promise<void> {
  const request =
    input.operation === "memory.prefetched"
      ? { ids: input.memories.map((memory) => memory.id) }
      : { id: input.skill.id };
  const context = beginTrace(
    input.binding,
    input.operation,
    request,
    "trusted-host",
  );
  if (!context) return;
  await deliver(context, () => {
    if (input.operation === "memory.prefetched") {
      const memories = input.memories.map((memory) => ({
        id: memory.id,
        contentDigest: verifiedDigest(memory.contentDigest),
        layer: memory.layer,
        ...(memory.scope ? { scope: memory.scope } : {}),
        ...(memory.kind ? { kind: memory.kind } : {}),
      }));
      const evidence = { returnedCount: memories.length, memories };
      return {
        ...baseEvent(context, "ok", evidence),
        operation: "memory.prefetched",
        evidence,
      };
    }
    const evidence = {
      id: input.skill.id,
      contentDigest: verifiedDigest(input.skill.contentDigest),
      ...(input.skill.sourceDigest
        ? { sourceDigest: verifiedDigest(input.skill.sourceDigest) }
        : {}),
    };
    return {
      ...baseEvent(context, "ok", evidence),
      operation: "skill.loaded",
      evidence,
    };
  });
}

type TracedControlPlaneMeta = {
  target: AgentControlPlane;
  binding: WorkSemanticTraceBinding;
};

const tracedControlPlanes = new WeakMap<object, TracedControlPlaneMeta>();

/**
 * Decorate an in-process control plane with the same private evidence emitted
 * by the broker. This is used by trusted host runtimes (including evals) whose
 * MCP servers call the control plane directly.
 */
export function traceAgentControlPlane(
  controlPlane: AgentControlPlane,
  binding: WorkSemanticTraceBinding,
): AgentControlPlane {
  const existing = tracedControlPlanes.get(controlPlane);
  if (
    existing &&
    existing.binding.runId === binding.runId &&
    existing.binding.orgId === binding.orgId &&
    existing.binding.kind === binding.kind
  ) {
    return controlPlane;
  }
  const target = existing?.target ?? controlPlane;
  const tracedMethods: Partial<AgentControlPlane> = {
    evaluateActionPolicy: (args) =>
      traceActionPolicy({
        binding,
        source: "trusted-host",
        request: {
          scope: args.scope,
          kind: args.kind,
          ...(args.target !== undefined ? { target: args.target } : {}),
          ...(args.riskLevel !== undefined ? { riskLevel: args.riskLevel } : {}),
        },
        execute: () => target.evaluateActionPolicy(args),
      }),
    searchWorkMemoryByContext: (args) =>
      traceMemorySearch({
        binding,
        source: "trusted-host",
        request: {
          query: args.query,
          ...(args.limit !== undefined ? { limit: args.limit } : {}),
        },
        execute: () => target.searchWorkMemoryByContext(args),
      }),
    searchLibraryForRun: (args) =>
      traceLibrarySearch({
        binding,
        source: "trusted-host",
        request: {
          query: args.query,
          ...(args.limit !== undefined ? { limit: args.limit } : {}),
        },
        execute: () => target.searchLibraryForRun(args),
      }),
    listWorkflowsWithTriggers: (args) =>
      traceWorkflowList({
        binding,
        source: "trusted-host",
        request: {
          ...(args.limit !== undefined ? { limit: args.limit } : {}),
        },
        execute: () => target.listWorkflowsWithTriggers(args),
      }),
    listGraphjinTools: (args) =>
      traceGraphjinToolsList({
        binding,
        source: "trusted-host",
        execute: () => target.listGraphjinTools(args),
      }),
    callGraphjinTool: (args) =>
      traceGraphjinToolCall({
        binding,
        source: "trusted-host",
        toolName: args.name,
        ...(args.arguments !== undefined
          ? { arguments: args.arguments }
          : {}),
        execute: () => target.callGraphjinTool(args),
      }),
    queryGraphjinRead: (args) =>
      traceGraphjinQuery({
        binding,
        source: "trusted-host",
        query: args.query,
        ...(args.variables !== undefined ? { variables: args.variables } : {}),
        ...(args.operationName !== undefined
          ? { operationName: args.operationName }
          : {}),
        execute: () => target.queryGraphjinRead(args),
      }),
  };
  const decorated = new Proxy(target, {
    get(value, property) {
      if (Object.prototype.hasOwnProperty.call(tracedMethods, property)) {
        return Reflect.get(tracedMethods, property, tracedMethods);
      }
      const original = Reflect.get(value, property, value);
      return typeof original === "function" ? original.bind(value) : original;
    },
  });
  tracedControlPlanes.set(decorated, { target, binding });
  return decorated;
}

/** Broker calls already emit at their outer trusted boundary. */
export function unwrapWorkSemanticTraceControlPlane(
  controlPlane: AgentControlPlane,
): AgentControlPlane {
  return tracedControlPlanes.get(controlPlane)?.target ?? controlPlane;
}

function graphjinOperation(
  toolName: string,
): "graphjin.execute" | "graphjin.catalog" | "graphjin.call" {
  if (toolName === "execute_graphql") return "graphjin.execute";
  if (toolName === "query_catalog") return "graphjin.catalog";
  return "graphjin.call";
}

function graphqlOperationType(
  query: string,
): "query" | "mutation" | "subscription" | "unknown" {
  const withoutComments = query.replace(/#[^\n\r]*/g, "").trimStart();
  if (withoutComments.startsWith("{")) return "query";
  const match = /^(query|mutation|subscription)\b/i.exec(withoutComments);
  return (match?.[1]?.toLowerCase() as
    | "query"
    | "mutation"
    | "subscription"
    | undefined) ?? "unknown";
}

function isToolResultError(result: unknown): boolean {
  const value = record(result);
  if (value?.isError === true) return true;
  const content = asArray(value?.content);
  return content.some((entry) => {
    const text = string(record(entry)?.text);
    if (!text) return false;
    try {
      const payload = record(JSON.parse(text));
      return Array.isArray(payload?.errors) && payload.errors.length > 0;
    } catch {
      return false;
    }
  });
}

function isGraphqlResultError(result: unknown): boolean {
  const errors = record(result)?.errors;
  return Array.isArray(errors) && errors.length > 0;
}

function canonicalJson(value: unknown): string {
  const seen = new WeakSet<object>();
  const normalize = (item: unknown): unknown => {
    if (item === null || typeof item === "string" || typeof item === "boolean") {
      return item;
    }
    if (typeof item === "number") {
      return Number.isFinite(item) ? item : String(item);
    }
    if (typeof item === "bigint") return item.toString();
    if (item instanceof Date) return item.toISOString();
    if (Array.isArray(item)) return item.map((entry) => normalize(entry));
    if (typeof item === "object") {
      if (seen.has(item)) return "[circular]";
      seen.add(item);
      const output: Record<string, unknown> = {};
      for (const key of Object.keys(item).sort()) {
        const child = (item as Record<string, unknown>)[key];
        if (
          child === undefined ||
          typeof child === "function" ||
          typeof child === "symbol"
        ) {
          continue;
        }
        output[key] = normalize(child);
      }
      seen.delete(item);
      return output;
    }
    return String(item);
  };
  return JSON.stringify(normalize(value)) ?? "null";
}

function record(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : undefined;
}

function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function string(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function number(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function verifiedDigest(value: string): string {
  if (!/^sha256:[0-9a-f]{64}$/.test(value)) {
    throw new Error("semantic evidence digest must be sha256-prefixed hex");
  }
  return value;
}
