import { startupPhase, withStartupTrace } from "@neko/telemetry/startup";
import { data_source, db, desc, eq, graphjinAgentEnabledForOrg } from "@neko/db";
import { errorCodeOf, observeSafely, type HarnessObserver } from "@neko/telemetry";
import {
  shellToolName,
  type AgentBackend,
  type AgentBackendId,
  type AgentEvent,
  type AgentRunResult,
  type AgentTokenUsage,
} from "./agent-backend";
import { resolveAgentBackend } from "./agent-backend-resolver";
import { parseJsonFromOutput } from "./agent-backends/output";
import { runValidatedAgentTurn } from "./agent-validate-loop";
import {
  buildDiscoveryPathwaysSection,
  getDiscoveryPathways,
} from "./discovery-pathways";
import {
  knowledgePackPaths,
  prefetchKnowledgeForOrg,
  readKnowledgePack,
} from "./knowledge-pack";
import { buildMetricPrompt } from "./metric-prompt";
import {
  formatGlobalMemoryPromptContext,
} from "./work/memory";
import { sandboxAgentBackendForJob } from "./work/sandbox-launcher";
import {
  ensureIsolatedJobWorkspace,
  ensureWorkWorkspace,
} from "./work/workspace";
import { graphjinAgentResponseStatus, normalizeGraphjinAgentUsage } from "./usage-normalization";
import { GRAPHJIN_AGENT_ASK_TOOL_TITLE, GRAPHJIN_EXECUTE_GRAPHQL_TOOL_TITLE } from "./graphjin/mcp-names";

// Keep in sync with ROLE_FOCUS (bootstrap-metrics-writer.ts) and the
// onboarding ALL_SEATS list — every seat the product offers must be here.
export const EXEC_ROLES = [
  "CEO",
  "CFO",
  "COO",
  "CRO",
  "CMO",
  "CIO",
  "CPO",
] as const;
export type ExecRole = (typeof EXEC_ROLES)[number];

export type MetricAgentInput = {
  orgId: string;
  /** Original ad-hoc user question; absent for preconfigured bootstrap cards. */
  question?: string;
  role: ExecRole;
  slug: string;
  title: string;
  why: string;
  chartHint: "kpi" | "line" | "bar" | "donut" | "area";
  jobId?: string;
  debug?: boolean;
  /** Optional provider-neutral observation stream shared by production and evals. */
  observer?: HarnessObserver;
  /** Additional metadata; the observer drops content-bearing and unsafe values. */
  observationAttributes?: Record<string, unknown>;
  /** Direct host-brokered GraphQL or read-only GraphJin server-agent delegation. */
  graphjinPath?: GraphjinDataPath;
  /** Host-only diagnostics hook; never serialized into an agent sandbox. */
  onDiagnostics?: (diagnostics: MetricAgentDiagnostics) => void;
  /** Host-only backend choice; the org's setting applies when absent. */
  backendId?: AgentBackendId;
  /** Host-only Ax model override, for evals. */
  model?: string;
  /** Host-only view of every agent event, for tracing. */
  onEvent?: (event: AgentEvent) => void;
};

export type { GraphjinDataPath } from "./work/graphjin-tool-policy";
import type { GraphjinDataPath } from "./work/graphjin-tool-policy";

export type MetricAgentDiagnostics = {
  graphjinPath: GraphjinDataPath;
  promptChars: number;
  durationMs: number;
  attempts: number;
  toolCalls: Record<string, number>;
  toolOutputBytes: number;
  /** Calls to GraphJin's agent, its model calls, time spent in them, and calls without an answer. */
  graphjinAgent?: { calls: number; modelCalls: number; durationMs: number; failures: number };
  outerUsage?: AgentTokenUsage;
  innerUsage?: AgentTokenUsage;
  totalUsage: AgentTokenUsage;
};

export const TIME_WINDOW_GRAINS = [
  "day",
  "week",
  "month",
  "quarter",
  "year",
  "all_time",
  "snapshot",
] as const;

export type TimeWindowGrain = (typeof TIME_WINDOW_GRAINS)[number];

export type TimeWindow = {
  grain: TimeWindowGrain;
  start: string | null;
  end: string | null;
  label: string;
};

export type MetricAgentResult = {
  reasoning: string;
  headlineMetric: string;
  headlineLabel: string;
  insightText: string;
  detailText: string;
  mood: "good" | "watch" | "bad";
  chartType: "kpi" | "line" | "bar" | "donut" | "area";
  chartData: Array<{ d: string; v: number; t?: number }>;
  baselineValue: number | null;
  timeWindow: TimeWindow;
};


export function runMetricAgent(input: MetricAgentInput): Promise<MetricAgentResult> {
  return withStartupTrace({ runId: input.jobId ?? input.slug, rootOperationId: `metric:${input.jobId ?? input.slug}`, observer: input.observer }, () => runMetricAgentTraced(input));
}

async function runMetricAgentTraced(
  input: MetricAgentInput,
): Promise<MetricAgentResult> {
  const observedStartedAt = Date.now();
  const graphjinPath: GraphjinDataPath = input.graphjinPath ??
    (await startupPhase("config.graphjin_agent", () => graphjinAgentEnabledForOrg(input.orgId)) ? "agent" : "direct");
  const sources = await startupPhase("metric.db_read", async () => db()
    .select({
      graphql_url: data_source.graphql_url,
      mcp_url: data_source.mcp_url,
    })
    .from(data_source)
    .where(eq(data_source.org_id, input.orgId))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1));
  const source = sources[0];
  const requiredUrl =
    graphjinPath === "agent" ? source?.graphql_url : source?.mcp_url;
  if (!requiredUrl) {
    throw new Error(
      `no ${graphjinPath === "agent" ? "graphql_url" : "mcp_url"} for org ${input.orgId}`,
    );
  }

  console.log(
    `[metric-agent] org=${input.orgId} role=${input.role} slug=${input.slug} graphjinPath=${graphjinPath}`,
  );

  const knowledgeWorkspace = await startupPhase("workspace.prepare", async () => ensureWorkWorkspace(
    input.orgId,
    "metric-agent",
    input.jobId ?? input.slug,
  ));
  const knowledge = await loadMetricKnowledge(input, knowledgeWorkspace.knowledgeRoot);

  const backend = await startupPhase("config.backend", async () => resolveAgentBackend(input.orgId, input.backendId, input.model));
  const debug = input.debug === true;
  const isolated = await startupPhase("workspace.isolate", async () => ensureIsolatedJobWorkspace(
    `metric-${input.jobId ?? input.slug}`,
  ));
  const operationId = `metric:${input.jobId ?? input.slug}`;
  const observe = async (
    event: Parameters<HarnessObserver["observe"]>[0],
  ): Promise<void> => observeSafely(input.observer, event);
  let promptChars = 0;
  let attempts = 0;
  let toolOutputBytes = 0;
  let outerUsage: AgentTokenUsage | undefined;
  let innerUsage: AgentTokenUsage | undefined;
  let innerUsageMissing = false;
  const toolCalls: Record<string, number> = {};
  const toolStarted = new Map<string, number>();
  const graphjinAgent = { calls: 0, modelCalls: 0, durationMs: 0, failures: 0 };
  let backendFailure: Pick<AgentRunResult, "errorCode" | "timedOut"> | undefined;
  await observe({
    kind: "run.start",
    timestamp: new Date(observedStartedAt).toISOString(),
    operationId,
    attributes: {
      "openneko.run.kind": "production",
      "openneko.product.path": "metric",
      "openneko.backend": backend.id,
      "openneko.data.path": `graphjin-${graphjinPath}`,
      ...(backend.model ? { "gen_ai.request.model": backend.model } : {}),
      ...input.observationAttributes,
    },
  });
  try {
    // Preload the top-5 global memories so pinned operator rules show up
    // verbatim. Anything narrower (per-card semantic match) is reachable
    // through the sandbox's search-only memory broker capability.
    const memoryContext = await startupPhase("context.memory", async () => formatGlobalMemoryPromptContext(input.orgId));
    const supportsMemorySearch = backend.capabilities.mcpTools;
    const sandboxedBackend = await startupPhase("sandbox.backend", async () => sandboxAgentBackendForJob({
      backend,
      orgId: input.orgId,
      runId: input.jobId ?? input.slug,
      workspace: isolated.workspace,
      access: {
        graphjinRead: graphjinPath === "direct",
        graphjinAgent: graphjinPath === "agent",
        memorySearch: supportsMemorySearch,
      },
    }));

    const basePrompt = buildMetricPrompt({
      input,
      knowledge,
      workspace: isolated.workspace,
      shellTool: shellToolName(backend.id),
      ...(graphjinPath === "direct"
        ? { queryTool: GRAPHJIN_EXECUTE_GRAPHQL_TOOL_TITLE }
        : { dataAgentTool: GRAPHJIN_AGENT_ASK_TOOL_TITLE }),
      memoryContext,
      supportsMemorySearch,
    });
    // GJ3: role-shaped warm-start for discovery (cached per org/role/intent).
    const pathwaysSection = graphjinPath === "agent" ? "" : buildDiscoveryPathwaysSection(
      getDiscoveryPathways({
        orgId: input.orgId,
        role: input.role,
        intent: input.title,
        insightsJson: knowledge.insights,
      }),
    );
    const prompt = pathwaysSection
      ? `${basePrompt}\n\n${pathwaysSection}`
      : basePrompt;

    console.log(
      `[metric-agent] org=${input.orgId} slug=${input.slug} backend=${backend.id} graphjinPath=${graphjinPath} runtime=openshell`,
    );

    promptChars = prompt.length;
    const startedAt = Date.now();
    const toolNames = new Map<string, string>();
    let firstOutputObserved = false;
    const observeFirstOutput = async (modelOperationId: string): Promise<void> => {
      if (firstOutputObserved) return;
      firstOutputObserved = true;
      const firstOutputMs = Date.now() - startedAt;
      await observe({
        kind: "model.first_chunk",
        operationId: modelOperationId,
        parentOperationId: `${operationId}:agent`,
        measurements: { firstOutputMs, coverage: "unavailable" },
      });
      await observe({
        kind: "run.first_output",
        operationId,
        measurements: { firstOutputMs, coverage: "unavailable" },
      });
    };
    await observe({
      kind: "stage.start",
      operationId: `${operationId}:agent`,
      parentOperationId: operationId,
      attributes: { "openneko.stage": "agent" },
    });
    const observedBackend: AgentBackend = {
      id: sandboxedBackend.id,
      model: sandboxedBackend.model,
      capabilities: sandboxedBackend.capabilities,
      async run(runOptions) {
        attempts += 1;
        const modelOperationId = `${operationId}:model:${attempts}`;
        if (attempts > 1) {
          await observe({
            kind: "retry",
            operationId: `${modelOperationId}:retry`,
            parentOperationId: `${operationId}:agent`,
            attributes: { "openneko.retry.reason": "validation" },
          });
        }
        await observe({
          kind: "model.request",
          operationId: modelOperationId,
          parentOperationId: `${operationId}:agent`,
          attributes: {
            "openneko.model.scope": "outer",
            "openneko.backend": backend.id,
            ...(backend.model ? { "gen_ai.request.model": backend.model } : {}),
          },
        });
        let sawOuterUsage = false;
        const callerOnEvent = runOptions.onEvent;
        const onEvent = async (event: AgentEvent): Promise<void> => {
          input.onEvent?.(event);
          if (event.type === "message" || event.type === "surface") {
            await observeFirstOutput(modelOperationId);
          }
          if (event.type === "tool_start") {
            toolNames.set(event.id, event.name);
            toolStarted.set(event.id, Date.now());
            toolCalls[event.name] = (toolCalls[event.name] ?? 0) + 1;
            await observe({
              kind: "tool.start",
              operationId: `${operationId}:tool:${event.id}`,
              parentOperationId: modelOperationId,
              attributes: { "gen_ai.tool.name": event.name },
              measurements: {
                inputBytes: byteLength(event.input),
                coverage: "unavailable",
              },
            });
            if (isGraphjinAgentTool(event.name)) {
              await observe({
                kind: "delegation.start",
                operationId: `${operationId}:delegation:${event.id}`,
                parentOperationId: modelOperationId,
                attributes: { "openneko.delegation.target": "graphjin-agent" },
              });
              await observe({
                kind: "model.request",
                operationId: `${operationId}:inner-model:${event.id}`,
                parentOperationId: `${operationId}:delegation:${event.id}`,
                attributes: { "openneko.model.scope": "inner" },
              });
            }
          } else if (event.type === "tool_end") {
            const toolName = toolNames.get(event.id) ?? "unknown";
            const outputBytes = byteLength(event.result ?? event.error);
            toolOutputBytes += outputBytes;
            await observe({
              kind: "tool.end",
              operationId: `${operationId}:tool:${event.id}`,
              parentOperationId: modelOperationId,
              status: event.error ? "error" : "ok",
              attributes: { "gen_ai.tool.name": toolName },
              measurements: { outputBytes, coverage: "unavailable" },
            });
            if (isGraphjinAgentTool(toolName)) {
              const parsedInner = findGraphjinUsage(event.result);
              graphjinAgent.calls += 1;
              graphjinAgent.modelCalls += parsedInner?.modelCalls ?? 0;
              graphjinAgent.durationMs += Date.now() - (toolStarted.get(event.id) ?? Date.now());
              if (event.error || graphjinAgentResponseStatus(event.result) !== "answered") graphjinAgent.failures += 1;
              if (parsedInner) {
                innerUsage = addUsage(innerUsage, parsedInner.usage);
                await observe({
                  kind: "model.response",
                  operationId: `${operationId}:inner-model:${event.id}`,
                  parentOperationId: `${operationId}:delegation:${event.id}`,
                  status: event.error ? "error" : "ok",
                  attributes: {
                    "openneko.model.scope": "inner",
                    ...(parsedInner.provider
                      ? { "gen_ai.provider.name": parsedInner.provider }
                      : {}),
                    ...(parsedInner.model
                      ? { "gen_ai.response.model": parsedInner.model }
                      : {}),
                  },
                  measurements: parsedInner.usage,
                });
              } else {
                innerUsageMissing = true;
                await observe({
                  kind: "model.response",
                  operationId: `${operationId}:inner-model:${event.id}`,
                  parentOperationId: `${operationId}:delegation:${event.id}`,
                  status: event.error ? "error" : "ok",
                  attributes: { "openneko.model.scope": "inner" },
                  measurements: {
                    coverage: "unavailable",
                    missingReasons: ["GraphJin agent response omitted normalized usage"],
                  },
                });
              }
              await observe({
                kind: "delegation.end",
                operationId: `${operationId}:delegation:${event.id}`,
                parentOperationId: modelOperationId,
                status: event.error ? "error" : "ok",
                attributes: { "openneko.delegation.target": "graphjin-agent" },
              });
            }
          } else if (event.type === "usage" && event.source === "outer") {
            sawOuterUsage = true;
            outerUsage = addUsage(outerUsage, event.usage);
            await observe({
              kind: "model.response",
              operationId: modelOperationId,
              parentOperationId: `${operationId}:agent`,
              status: "ok",
              attributes: {
                "openneko.model.scope": "outer",
                ...(event.provider
                  ? { "gen_ai.provider.name": event.provider }
                  : {}),
                ...(event.model ? { "gen_ai.response.model": event.model } : {}),
              },
              measurements: event.usage,
            });
          }
          await callerOnEvent?.(event);
        };
        const result = await sandboxedBackend.run({ ...runOptions, onEvent });
        backendFailure = result.status === "failed"
          ? { errorCode: result.errorCode, timedOut: result.timedOut }
          : undefined;
        if (!sawOuterUsage) {
          await observe({
            kind: "model.response",
            operationId: modelOperationId,
            parentOperationId: `${operationId}:agent`,
            status: result.status === "completed" ? "ok" : "error",
            attributes: { "openneko.model.scope": "outer" },
            measurements: {
              coverage: "unavailable",
              missingReasons: ["agent backend returned no normalized usage"],
            },
          });
        }
        return result;
      },
    };
    // GJ2: iterative validation loop — a malformed reply is fed back to the
    // agent for a corrective turn instead of failing the whole job.
    const { value: parsed } = await runValidatedAgentTurn<
      Partial<MetricAgentResult>
    >({
      backend: observedBackend,
      run: {
        prompt,
        orgId: input.orgId,
        tag: input.jobId,
        workspace: isolated.workspace,
        debug,
      },
      maxAttempts: graphjinPath === "agent" ? 2 : undefined,
      label: `metric-agent org=${input.orgId} slug=${input.slug}`,
      validate: (finalText) => {
        const out = parseJsonFromOutput(
          finalText,
        ) as Partial<MetricAgentResult>;
        if (!out.headlineMetric && !out.insightText) {
          throw new Error(
            "JSON parsed but headlineMetric and insightText are both empty",
          );
        }
        return out;
      },
    });
    const elapsedSec = ((Date.now() - startedAt) / 1000).toFixed(0);
    const totalUsage = combinedUsage(
      outerUsage,
      innerUsage,
      graphjinPath === "agent" && (innerUsageMissing || !innerUsage),
    );
    await observe({
      kind: "stage.end",
      operationId: `${operationId}:agent`,
      parentOperationId: operationId,
      status: "ok",
      attributes: { "openneko.stage": "agent" },
      measurements: {
        durationMs: Date.now() - startedAt,
        coverage: totalUsage.coverage,
        ...(totalUsage.missingReasons
          ? { missingReasons: totalUsage.missingReasons }
          : {}),
      },
    });

    const tw = (parsed.timeWindow ?? {}) as Partial<TimeWindow>;
    const result: MetricAgentResult = {
      reasoning: String(parsed.reasoning ?? ""),
      headlineMetric: String(parsed.headlineMetric ?? ""),
      headlineLabel: String(parsed.headlineLabel ?? ""),
      insightText: String(parsed.insightText ?? ""),
      detailText: String(parsed.detailText ?? ""),
      mood: parsed.mood as MetricAgentResult["mood"],
      chartType: parsed.chartType as MetricAgentResult["chartType"],
      chartData: Array.isArray(parsed.chartData)
        ? (parsed.chartData as MetricAgentResult["chartData"])
        : [],
      baselineValue: typeof parsed.baselineValue === "number" && Number.isFinite(parsed.baselineValue)
        ? parsed.baselineValue
        : null,
      timeWindow: {
        grain: tw.grain as TimeWindowGrain,
        start: tw.start === null ? null : tw.start ? String(tw.start) : null,
        end: tw.end === null ? null : tw.end ? String(tw.end) : null,
        label: String(tw.label ?? ""),
      },
    };

    console.log(
      `[metric-agent] org=${input.orgId} slug=${input.slug} done in ${elapsedSec}s`,
    );

    await observe({
      kind: "validation.result",
      operationId: `${operationId}:validation`,
      parentOperationId: operationId,
      status: "ok",
      attributes: { "openneko.validation.contract": "metric-result" },
    });
    await observe({
      kind: "run.end",
      operationId,
      status: "ok",
      attributes: {
        "openneko.outcome": "completed",
        ...input.observationAttributes,
      },
      measurements: {
        durationMs: Date.now() - observedStartedAt,
        coverage: totalUsage.coverage,
        ...(totalUsage.missingReasons
          ? { missingReasons: totalUsage.missingReasons }
          : {}),
      },
    });

    return result;
  } catch (cause) {
    const totalUsage = combinedUsage(
      outerUsage,
      innerUsage,
      graphjinPath === "agent" && (innerUsageMissing || !innerUsage),
    );
    const errorCode = backendFailure?.errorCode ?? errorCodeOf(cause);
    const failure = {
      errorType: backendFailure?.errorCode ?? (cause instanceof Error ? cause.name : "unknown"),
      ...(errorCode ? { errorCode } : {}),
      errorMessage: cause instanceof Error ? cause.message : String(cause),
    };
    await observe({
      kind: "error",
      operationId: `${operationId}:error`,
      parentOperationId: operationId,
      status: "error",
      ...failure,
    });
    await observe({
      kind: "run.end",
      operationId,
      status: "error",
      ...failure,
      attributes: {
        "openneko.outcome": "failed",
        ...(backendFailure?.timedOut ? { "openneko.timed_out": true } : {}),
        ...input.observationAttributes,
      },
      measurements: {
        durationMs: Date.now() - observedStartedAt,
        coverage: totalUsage.coverage,
        missingReasons: [
          ...(totalUsage.missingReasons ?? []),
          "run failed before all telemetry was guaranteed complete",
        ],
      },
    });
    throw cause;
  } finally {
    try {
      input.onDiagnostics?.({
        graphjinPath,
        promptChars,
        durationMs: Date.now() - observedStartedAt,
        attempts,
        toolCalls: { ...toolCalls },
        toolOutputBytes,
        ...(graphjinPath === "agent" ? { graphjinAgent: { ...graphjinAgent } } : {}),
        ...(outerUsage ? { outerUsage } : {}),
        ...(innerUsage ? { innerUsage } : {}),
        totalUsage: combinedUsage(
          outerUsage,
          innerUsage,
          graphjinPath === "agent" && (innerUsageMissing || !innerUsage),
        ),
      });
    } catch {
      // Diagnostics must never change the metric result.
    } finally {
      await isolated.cleanup();
    }
  }
}

function byteLength(value: unknown): number {
  if (value === undefined) return 0;
  try {
    return Buffer.byteLength(
      typeof value === "string" ? value : JSON.stringify(value),
      "utf8",
    );
  } catch {
    return 0;
  }
}

function isGraphjinAgentTool(name: string): boolean {
  return name.toLocaleLowerCase().includes("neko_graphjin_agent");
}

function addOptional(
  left: number | undefined,
  right: number | undefined,
): number | undefined {
  if (left === undefined && right === undefined) return undefined;
  return (left ?? 0) + (right ?? 0);
}

function addUsage(
  current: AgentTokenUsage | undefined,
  next: AgentTokenUsage,
): AgentTokenUsage {
  const missingReasons = [
    ...(current?.missingReasons ?? []),
    ...(next.missingReasons ?? []),
  ];
  return {
    inputTokens: addOptional(current?.inputTokens, next.inputTokens),
    outputTokens: addOptional(current?.outputTokens, next.outputTokens),
    cacheReadTokens: addOptional(current?.cacheReadTokens, next.cacheReadTokens),
    cacheWriteTokens: addOptional(current?.cacheWriteTokens, next.cacheWriteTokens),
    reasoningTokens: addOptional(current?.reasoningTokens, next.reasoningTokens),
    totalTokens: addOptional(current?.totalTokens, next.totalTokens),
    estimatedCostUsd: addOptional(
      current?.estimatedCostUsd,
      next.estimatedCostUsd,
    ),
    billedCostUsd: addOptional(current?.billedCostUsd, next.billedCostUsd),
    ...(next.currency ?? current?.currency
      ? { currency: next.currency ?? current?.currency }
      : {}),
    ...(next.pricingCatalogVersion ?? current?.pricingCatalogVersion
      ? {
          pricingCatalogVersion:
            next.pricingCatalogVersion ?? current?.pricingCatalogVersion,
        }
      : {}),
    coverage:
      current?.coverage === "partial" ||
      current?.coverage === "unavailable" ||
      next.coverage !== "complete"
        ? "partial"
        : "complete",
    ...(missingReasons.length > 0
      ? { missingReasons: [...new Set(missingReasons)] }
      : {}),
  };
}

function combinedUsage(
  outer: AgentTokenUsage | undefined,
  inner: AgentTokenUsage | undefined,
  innerMissing: boolean,
): AgentTokenUsage {
  let total = outer;
  if (inner) total = addUsage(total, inner);
  if (!total) {
    return {
      coverage: "unavailable",
      missingReasons: [
        "agent backend returned no normalized usage",
        ...(innerMissing ? ["GraphJin agent usage is unavailable"] : []),
      ],
    };
  }
  if (!innerMissing) return total;
  return {
    ...total,
    coverage: "partial",
    missingReasons: [
      ...new Set([
        ...(total.missingReasons ?? []),
        "GraphJin agent usage is unavailable",
      ]),
    ],
  };
}

const findGraphjinUsage = normalizeGraphjinAgentUsage;

async function loadMetricKnowledge(input: MetricAgentInput, knowledgeRoot: string) {
  const refreshResult = await startupPhase("knowledge.prefetch", async () => prefetchKnowledgeForOrg(
    input.orgId,
    knowledgeRoot,
  ));
  if (refreshResult.ok) {
    const totalBytes = refreshResult.files.reduce((n, f) => n + f.bytes, 0);
    console.log(
      `[metric-agent] org=${input.orgId} slug=${input.slug} knowledge refreshed (${refreshResult.files.length} files, ${totalBytes}B)`,
    );
  } else {
    console.warn(
      `[metric-agent] org=${input.orgId} slug=${input.slug} knowledge refresh failed (${refreshResult.error}); proceeding with on-disk pack`,
    );
  }
  return startupPhase("knowledge.read_pack", async () => readKnowledgePack(
    knowledgePackPaths(knowledgeRoot),
  ));
}
