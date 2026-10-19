import { randomUUID } from "node:crypto";
import { cp, mkdtemp, readFile, rm } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import {
  EvalEnvironmentError,
  EvalTaskError,
  createScore,
  estimateUsageCost,
  resolveCredentialRef,
  textDigest,
  type EvalDriver,
  type EvalExecution,
  type LoadedCase,
  type LoadedEval,
  type EvalPlan,
  type PricingCatalog,
  type EvalVariant,
} from "@neko/evals";
import {
  createTestOrg,
  deleteTestOrg,
  seedDataSource,
  seedProvider,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { and, buildPoolConfig, db, eq, llm_provider_config, reconnectPool } from "@neko/db";
import {
  classifyQuestion,
  combineAgentTokenUsage,
  ensureHostConfigProvisioned,
  provisionHostConfig,
  getGraphjinAgentStatus,
  runMetricAgent,
  shutdownAgentBroker,
  type AgentTokenUsage,
  type MetricAgentDiagnostics,
  type MetricAgentResult,
  type QuestionClassificationDiagnostics,
} from "@neko/llm";
import { maybeEncryptSecret } from "@neko/llm/secrets";
import pg from "pg";
import { validateResult } from "../src/jobs/metric-refresh.js";

type AdventureWorksOracle = {
  anchorDate: string;
  startDate: string;
  expectedValue: number;
  baselineValue: number;
  expectedDimension?: string;
};

type VariantRuntime = {
  orgId: string;
  environment: Partial<Record<RuntimeEnvironmentName, string>>;
};

function costMeasurements(input: {
  variant: EvalVariant;
  catalog: PricingCatalog | undefined;
  outerUsage: AgentTokenUsage;
  innerUsage?: AgentTokenUsage;
}): Record<string, unknown> {
  const scopes = [
    {
      name: "outer",
      model: input.variant.outer_model,
      usage: input.outerUsage,
    },
    ...(input.variant.data_path === "graphjin-agent" && input.variant.inner_model
      ? [
          {
            name: "inner",
            model: input.variant.inner_model,
            usage: input.innerUsage,
          },
        ]
      : []),
  ];
  const priced = scopes.map((scope) => ({
    ...scope,
    estimate: estimateUsageCost({
      usage: scope.usage,
      provider: scope.model.provider,
      model: scope.model.model,
      catalog: input.catalog,
    }),
  }));
  const scopeCovered = (scope: (typeof priced)[number]) =>
    scope.usage?.billedCostUsd !== undefined ||
    scope.estimate.coverage === "complete";
  const covered = priced.filter(scopeCovered).length;
  const available = priced.filter(
    (scope) =>
      scope.usage?.billedCostUsd !== undefined ||
      scope.estimate.estimatedCostUsd !== undefined,
  ).length;
  const estimated = priced
    .map((scope) => scope.estimate.estimatedCostUsd)
    .filter((value): value is number => value !== undefined);
  const reasons = priced.flatMap((scope) =>
    scopeCovered(scope)
      ? []
      : (scope.estimate.missingReasons ?? ["cost unavailable"]).map(
          (reason) => `${scope.name}: ${reason}`,
        ),
  );
  const costCoverage =
    covered === scopes.length
      ? "complete"
      : available > 0
        ? "partial"
        : "unavailable";
  return {
    costCoverage,
    ...(estimated.length > 0
      ? { estimatedCostUsd: estimated.reduce((sum, value) => sum + value, 0) }
      : {}),
    ...(estimated.length > 0 && input.catalog
      ? {
          currency: input.catalog.currency,
          pricingCatalogVersion: input.catalog.version,
        }
      : {}),
    ...(reasons.length > 0 ? { costMissingReasons: reasons } : {}),
  };
}

const HEADLINE_PATTERN = /^([+-]?\d+(?:\.\d+)?)\s*(?:(thousand|million|billion)\b|([kmb])\b)?\s*(?:[%x]|[a-z][a-z\s]*)?$/iu;
const RUNTIME_ENVIRONMENT_NAMES = [
  "XDG_CONFIG_HOME",
  "OPENNEKO_AGENT_MODEL_PROVIDER",
  "OPENNEKO_AGENT_MODEL_HOST",
  "OPENNEKO_AGENT_MODEL_KEY_ENV",
  "NEKO_PG_HOST",
  "NEKO_PG_PORT",
  "NEKO_PG_USER",
  "NEKO_PG_PASSWORD",
  "NEKO_PG_DATABASE",
  "NEKO_PG_SSLMODE",
] as const;
type RuntimeEnvironmentName = (typeof RUNTIME_ENVIRONMENT_NAMES)[number];

const INFRASTRUCTURE_ERROR = /openshell|sandbox|gateway|ECONNREFUSED|socket hang up/i;

/** An agent that runs but gives no valid answer fails the case; infrastructure errors still pause the run. */
function agentFailure(cause: unknown, wallDurationMs: number, diagnostics?: MetricAgentDiagnostics): unknown {
  const message = cause instanceof Error ? cause.message : String(cause);
  if (cause instanceof EvalEnvironmentError || INFRASTRUCTURE_ERROR.test(message)) return cause;
  const agent = diagnostics?.graphjinAgent;
  const usage = diagnostics?.totalUsage;
  return new EvalTaskError(message, "agent_failure", {
    measurements: {
      wallDurationMs,
      ...(usage?.inputTokens !== undefined ? { inputTokens: usage.inputTokens } : {}),
      ...(usage?.outputTokens !== undefined ? { outputTokens: usage.outputTokens } : {}),
      ...(usage?.totalTokens !== undefined ? { totalTokens: usage.totalTokens } : {}),
      ...(agent
        ? {
            graphjinAgentCalls: agent.calls,
            graphjinAgentModelCalls: agent.modelCalls,
            graphjinAgentDurationMs: agent.durationMs,
            graphjinAgentFailures: agent.failures,
          }
        : {}),
    },
  });
}

function parseHeadline(value: string): number | null {
  const match = value.replace(/[$,]/gu, "").trim().match(HEADLINE_PATTERN);
  if (!match) return null;
  let number = Number.parseFloat(match[1] ?? "");
  if (!Number.isFinite(number)) return null;
  const scale = (match[2] ?? match[3] ?? "").toLowerCase().charAt(0);
  if (scale === "t" || scale === "k") number *= 1_000;
  if (scale === "m") number *= 1_000_000;
  if (scale === "b") number *= 1_000_000_000;
  return number;
}

function normalizedProvider(value: string): string {
  const normalized = value.trim().toLocaleLowerCase();
  if (["google", "google-gemini", "gemini"].includes(normalized)) return "gemini";
  return normalized;
}

function datasetSetting(
  loaded: LoadedEval,
  datasetId: string,
  field: "agent_graphql_ref" | "agent_mcp_ref" | "oracle_database_ref",
  defaultKey: string,
): string {
  const dataset = loaded.datasets.get(datasetId);
  if (!dataset) throw new Error(`dataset ${datasetId} is not loaded`);
  const ref = dataset.connection[field];
  const fromEnv = ref ? process.env[ref.slice("env:".length)]?.trim() : undefined;
  const fallback = dataset.connection.defaults?.[defaultKey];
  const value = fromEnv || fallback;
  if (!value) throw new EvalEnvironmentError(`${datasetId} has no ${field}`, "dataset_connection_missing");
  return value;
}

function outputOf(execution: EvalExecution): MetricAgentResult {
  if (!execution.output || typeof execution.output !== "object") {
    throw new Error("metric execution did not retain a structured output");
  }
  return execution.output as MetricAgentResult;
}

export function createAdventureWorksMetricDriver(context: {
  loaded: LoadedEval;
  plan: EvalPlan;
}): EvalDriver {
  // Capture metadata DB settings before XDG_CONFIG_HOME is isolated below.
  // Local OpenNeko installs may have rotated the Compose password via /setup;
  // hiding their local config without preserving the resolved connection makes
  // every model call fail while creating its isolated eval organization.
  const metadataDbConfig = buildPoolConfig();
  const hostConfigRoot =
    process.env.XDG_CONFIG_HOME?.trim() || join(homedir(), ".config");
  const runtimes = new Map<string, VariantRuntime>();
  let pool: pg.Pool | undefined;
  let isolatedConfigRoot: string | undefined;
  const originalEnv = new Map<string, string | undefined>();
  const mutableEnv = RUNTIME_ENVIRONMENT_NAMES;
  for (const name of mutableEnv) originalEnv.set(name, process.env[name]);

  const oracleUrl = datasetSetting(
    context.loaded,
    context.loaded.cases[0]?.dataset ?? "adventureworks",
    "oracle_database_ref",
    "oracle_database_url",
  );

  function oraclePool(): pg.Pool {
    pool ??= new pg.Pool({ connectionString: oracleUrl, max: 1 });
    return pool;
  }

  async function prepareVariant(variant: EvalVariant): Promise<VariantRuntime> {
    const existing = runtimes.get(variant.id);
    if (existing) {
      for (const name of mutableEnv) {
        const value = existing.environment[name];
        if (value === undefined) delete process.env[name];
        else process.env[name] = value;
      }
      return existing;
    }
    const apiKey = resolveCredentialRef(variant.outer_model.credential_ref);
    if (!apiKey) {
      throw new EvalEnvironmentError(
        `variant ${variant.id} has no credential_ref`,
        "provider_credential_missing",
      );
    }
    const datasetId = context.loaded.cases[0]?.dataset ?? "adventureworks";
    const graphqlUrl = datasetSetting(
      context.loaded,
      datasetId,
      "agent_graphql_ref",
      "agent_graphql_url",
    );
    const mcpUrl = datasetSetting(
      context.loaded,
      datasetId,
      "agent_mcp_ref",
      "agent_mcp_url",
    );
    if (!isolatedConfigRoot) {
      isolatedConfigRoot = await mkdtemp(join(tmpdir(), "openneko-eval-config-"));
      // OpenShell's selected-gateway registration also lives below XDG config.
      // Mirror that public connection metadata (and any client credentials it
      // references) into the private temporary root so sandbox execution keeps
      // using the operator-selected gateway without mutating the host config.
      // A shared org also needs the host secret key that encrypted its secrets.
      for (const dir of process.env.OPENNEKO_EVAL_ORG_ID ? ["openshell", "openneko"] : ["openshell"]) {
        await cp(
          join(hostConfigRoot, dir),
          join(isolatedConfigRoot, dir),
          { recursive: true, force: false, errorOnExist: false },
        ).catch((cause: NodeJS.ErrnoException) => {
          if (cause.code !== "ENOENT") throw cause;
        });
      }
    }
    process.env.XDG_CONFIG_HOME = isolatedConfigRoot;
    for (const name of mutableEnv.slice(1)) delete process.env[name];
    if (metadataDbConfig.host) process.env.NEKO_PG_HOST = metadataDbConfig.host;
    if (metadataDbConfig.port) process.env.NEKO_PG_PORT = String(metadataDbConfig.port);
    if (metadataDbConfig.user) process.env.NEKO_PG_USER = metadataDbConfig.user;
    if (typeof metadataDbConfig.password === "string") {
      process.env.NEKO_PG_PASSWORD = metadataDbConfig.password;
    }
    if (metadataDbConfig.database) {
      process.env.NEKO_PG_DATABASE = metadataDbConfig.database;
    }
    if (metadataDbConfig.ssl) process.env.NEKO_PG_SSLMODE = "require";
    process.env.OPENNEKO_AGENT_MODEL_PROVIDER = `openneko-eval-${variant.id}`;

    // A shared org reuses the org the running GraphJin signs tokens for.
    const sharedOrg = process.env.OPENNEKO_EVAL_ORG_ID;
    const orgId = sharedOrg ?? uniqueOrgId(`eval-${variant.id}`);
    if (!sharedOrg) await createTestOrg(orgId, `Eval ${variant.id}`);
    try {
      if (sharedOrg) {
        const [primary] = await db().select({ model: llm_provider_config.model }).from(llm_provider_config)
          .where(and(eq(llm_provider_config.org_id, orgId), eq(llm_provider_config.scope, "primary")));
        if (primary?.model !== variant.outer_model.model) {
          throw new EvalEnvironmentError(`org ${orgId} uses ${primary?.model}, variant wants ${variant.outer_model.model}`, "outer_model_mismatch");
        }
      } else {
        await seedDataSource(orgId, {
          graphqlUrl,
          mcpUrl,
          label: "AdventureWorks eval",
        });
        await seedProvider(orgId, {
          scope: "primary",
          provider: variant.outer_model.provider,
          model: variant.outer_model.model,
          enabled: true,
          config: variant.outer_model.config,
          secrets: { apiKey: maybeEncryptSecret(apiKey) },
        });
        await seedProvider(orgId, {
          scope: "agent",
          provider: variant.backend,
          enabled: true,
          config: { backend: variant.backend },
        });
      }
      if (sharedOrg) await ensureHostConfigProvisioned(orgId);
      else await provisionHostConfig(orgId, { requireOpenShellSync: true });
    } catch (cause) {
      if (!sharedOrg) await deleteTestOrg(orgId).catch(() => {});
      throw cause;
    }
    const environment: Partial<Record<RuntimeEnvironmentName, string>> = {};
    for (const name of mutableEnv) {
      const value = process.env[name];
      if (value !== undefined) environment[name] = value;
    }
    const runtime = { orgId, environment };
    runtimes.set(variant.id, runtime);
    return runtime;
  }

  return {
    id: "adventureworks.metric",
    scorer: {
      id: "adventureworks.metric",
      version: "2.0.0",
      definition: {
        classification: "natural question to card metadata via production classifier",
        numeric: "relative error against host SQL",
        baseline: "baselineValue (else the first chart target) relative error against host SQL",
        timeWindow: "exact grain/start/end",
        contract: "OpenNeko metric result contract",
      },
    },
    async preflight({ loaded, plan }) {
      if (plan.slots.some((slot) => slot.case.product_path !== "metric")) {
        throw new EvalEnvironmentError(
          "adventureworks.metric only supports the metric product path",
          "adapter_incompatible",
        );
      }
      for (const variant of loaded.config.variants) {
        if (
          variant.data_path !== "graphjin-direct" &&
          variant.data_path !== "graphjin-agent"
        ) {
          throw new EvalEnvironmentError(
            `adventureworks.metric does not support ${variant.data_path}`,
            "data_path_unsupported",
          );
        }
        resolveCredentialRef(variant.outer_model.credential_ref);
        if (variant.data_path === "graphjin-agent" && !variant.inner_model) {
          throw new EvalEnvironmentError(
            `variant ${variant.id} must declare inner_model for graphjin-agent`,
            "inner_model_missing",
          );
        }
      }
      const datasetId = loaded.cases[0]?.dataset ?? "adventureworks";
      const graphqlUrl = datasetSetting(
        loaded,
        datasetId,
        "agent_graphql_ref",
        "agent_graphql_url",
      );
      const response = await fetch(graphqlUrl, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ query: "query OpenNekoEvalHealth { __typename }" }),
        signal: AbortSignal.timeout(5_000),
      }).catch((cause) => {
        throw new EvalEnvironmentError(
          `GraphJin preflight failed: ${cause instanceof Error ? cause.message : cause}`,
          "graphjin_unreachable",
        );
      });
      if (!response.ok) {
        throw new EvalEnvironmentError(
          `GraphJin preflight returned HTTP ${response.status}`,
          "graphjin_unhealthy",
        );
      }
      let delegatedStatus:
        | Awaited<ReturnType<typeof getGraphjinAgentStatus>>
        | undefined;
      if (loaded.config.variants.some((variant) => variant.data_path === "graphjin-agent")) {
        delegatedStatus = await getGraphjinAgentStatus({
          baseUrl: graphqlUrl,
          token: "openneko-read-only",
          signal: AbortSignal.timeout(10_000),
        }).catch((cause) => {
          throw new EvalEnvironmentError(
            `GraphJin agent preflight failed: ${cause instanceof Error ? cause.message : cause}`,
            "graphjin_agent_unreachable",
          );
        });
        if (!delegatedStatus.ready) {
          throw new EvalEnvironmentError(
            delegatedStatus.message || "GraphJin agent is not ready",
            "graphjin_agent_unready",
          );
        }
        if (!delegatedStatus.read_only) {
          throw new EvalEnvironmentError(
            "GraphJin agent must be globally read-only for harness evaluation",
            "graphjin_agent_not_read_only",
          );
        }
        for (const variant of loaded.config.variants.filter(
          (candidate) => candidate.data_path === "graphjin-agent",
        )) {
          const expected = variant.inner_model!;
          if (
            normalizedProvider(expected.provider) !==
              normalizedProvider(delegatedStatus.provider) ||
            (delegatedStatus.model && delegatedStatus.model !== expected.model)
          ) {
            throw new EvalEnvironmentError(
              `variant ${variant.id} requests inner ${expected.provider}:${expected.model}, GraphJin reports ${delegatedStatus.provider}:${delegatedStatus.model ?? "unknown"}`,
              "inner_model_mismatch",
            );
          }
        }
      }
      const fingerprint = await oraclePool().query<{
        database_name: string;
        server_version: string;
        anchor_date: string;
        orders: string;
        details: string;
        schema_digest: string;
      }>(`
        select
          current_database() as database_name,
          current_setting('server_version') as server_version,
          (select max(orderdate)::date::text from sales.salesorderheader) as anchor_date,
          (select count(*)::text from sales.salesorderheader) as orders,
          (select count(*)::text from sales.salesorderdetail) as details,
          (select md5(string_agg(table_schema || '.' || table_name || '.' || column_name || ':' || data_type, ',' order by table_schema, table_name, ordinal_position))
             from information_schema.columns
            where table_schema in ('sales', 'production', 'purchasing', 'person')) as schema_digest
      `);
      const endpoint = new URL(graphqlUrl);
      endpoint.username = "";
      endpoint.password = "";
      endpoint.search = "";
      endpoint.hash = "";
      return {
        datasetFingerprint: {
          dataset: datasetId,
          snapshot: loaded.datasetSnapshots.get(datasetId),
          agentEndpoint: endpoint.toString(),
          ...fingerprint.rows[0],
        },
        resolvedVariants: Object.fromEntries(
          loaded.config.variants.map((variant) => [
            variant.id,
            {
              backend: variant.backend,
              provider: variant.outer_model.provider,
              model: variant.outer_model.model,
              dataPath: variant.data_path,
              ...(variant.data_path === "graphjin-agent" && delegatedStatus
                ? {
                    innerProvider: delegatedStatus.provider,
                    innerModel: delegatedStatus.model,
                    innerMaxSteps: delegatedStatus.max_steps,
                  }
                : {}),
            },
          ]),
        ),
      };
    },
    async resolveOracle(evalCase: LoadedCase) {
      if (evalCase.oracle?.kind !== "sql.metric" || !evalCase.oraclePath) {
        throw new EvalEnvironmentError(
          `${evalCase.id} requires a sql.metric oracle ref`,
          "oracle_invalid",
        );
      }
      const sql = await readFile(evalCase.oraclePath, "utf8");
      const result = await oraclePool().query<{
        anchor_date: string;
        start_date: string;
        expected_value: string;
        baseline_value: string;
        expected_dimension: string | null;
      }>(sql);
      const row = result.rows[0];
      const oracle: AdventureWorksOracle = {
        anchorDate: row?.anchor_date ?? "",
        startDate: row?.start_date ?? "",
        expectedValue: Number(row?.expected_value),
        baselineValue: Number(row?.baseline_value),
        ...(row?.expected_dimension
          ? { expectedDimension: row.expected_dimension }
          : {}),
      };
      if (
        !oracle.anchorDate ||
        !oracle.startDate ||
        !Number.isFinite(oracle.expectedValue) ||
        !Number.isFinite(oracle.baselineValue)
      ) {
        throw new EvalEnvironmentError(
          `${evalCase.id} oracle returned no usable row`,
          "oracle_invalid",
        );
      }
      return { ...oracle, oracleSqlDigest: textDigest(sql) };
    },
    async execute({ runId, slot, observer }) {
      const runtime = await prepareVariant(slot.variant);
      const input = slot.case.input as {
        question?: unknown;
        role?: unknown;
      };
      const question = String(input.question ?? "").trim();
      const role = String(input.role ?? "").trim();
      if (!question) throw new Error(`${slot.caseId} has no natural-language question`);
      if (!["CEO", "CFO", "COO", "CRO", "CMO", "CIO", "CPO"].includes(role)) {
        throw new Error(`${slot.caseId} has an invalid executive role`);
      }
      const started = Date.now();
      const jobId = randomUUID();
      const operationId = `metric:${runId}-${slot.caseId}-${slot.repetition}`;
      let classificationDiagnostics: QuestionClassificationDiagnostics | undefined;
      let diagnostics: MetricAgentDiagnostics | undefined;
      const card = await classifyQuestion(question, role, runtime.orgId, {
        observer,
        operationId: `${operationId}:classification`,
        parentOperationId: operationId,
        observationAttributes: {
          "openneko.run.kind": "eval",
          "openneko.model.scope": "classification",
          "gen_ai.provider.name": slot.variant.outer_model.provider,
          "gen_ai.request.model": slot.variant.outer_model.model,
          "openneko.eval.case.id": slot.caseId,
          "openneko.eval.variant.id": slot.variantId,
        },
        onDiagnostics(value) {
          classificationDiagnostics = value;
        },
      });
      const chartHint = ["kpi", "line", "bar", "donut", "area"].includes(
        card.chartHint,
      )
        ? card.chartHint
        : "bar";
      const output = await runMetricAgent({
        orgId: runtime.orgId,
        question,
        debug: process.env.OPENNEKO_EVAL_DEBUG === "1",
        role: role as
          | "CEO"
          | "CFO"
          | "COO"
          | "CRO"
          | "CMO"
          | "CIO"
          | "CPO",
        slug: card.slug || `chat-${slot.caseId}`,
        title: card.title || question.slice(0, 80),
        why: card.why || question,
        chartHint: chartHint as
          | "kpi"
          | "line"
          | "bar"
          | "donut"
          | "area",
        jobId,
        graphjinPath:
          slot.variant.data_path === "graphjin-agent" ? "agent" : "direct",
        onDiagnostics(value) {
          diagnostics = value;
        },
        observer,
        observationAttributes: {
          "openneko.run.kind": "eval",
          "openneko.data.path": slot.variant.data_path,
          "gen_ai.provider.name": slot.variant.outer_model.provider,
          "gen_ai.request.model": slot.variant.outer_model.model,
          "openneko.eval.case.id": slot.caseId,
          "openneko.eval.variant.id": slot.variantId,
        },
      }).catch((cause: unknown) => {
        throw agentFailure(cause, Date.now() - started, diagnostics);
      });
      const wallDurationMs = Date.now() - started;
      const unavailableUsage = (reason: string): AgentTokenUsage => ({
        coverage: "unavailable",
        missingReasons: [reason],
      });
      const classifierUsage =
        classificationDiagnostics?.usage ??
        unavailableUsage("classification usage unavailable");
      const metricOuterUsage =
        diagnostics?.outerUsage ??
        unavailableUsage("metric-agent outer usage unavailable");
      const metricTotalUsage =
        diagnostics?.totalUsage ??
        unavailableUsage("metric-agent usage unavailable");
      const outerUsage = combineAgentTokenUsage(
        classifierUsage,
        metricOuterUsage,
      );
      const usage = combineAgentTokenUsage(classifierUsage, metricTotalUsage);
      const cost = costMeasurements({
        variant: slot.variant,
        catalog: context.loaded.pricing,
        outerUsage,
        ...(diagnostics?.innerUsage ? { innerUsage: diagnostics.innerUsage } : {}),
      });
      await observer.observe({
        kind: "output.contract",
        operationId: `${operationId}:contract`,
        parentOperationId: operationId,
        status: validateResult(output) ? "error" : "ok",
      });
      return {
        output,
        measurements: {
          wallDurationMs,
          ...(classificationDiagnostics
            ? { classificationDurationMs: classificationDiagnostics.durationMs }
            : {}),
          usageCoverage: usage.coverage,
          ...(usage.missingReasons
            ? { usageMissingReasons: usage.missingReasons }
            : {}),
          ...(usage.inputTokens !== undefined
            ? { inputTokens: usage.inputTokens }
            : {}),
          ...(usage.outputTokens !== undefined
            ? { outputTokens: usage.outputTokens }
            : {}),
          ...(usage.cacheReadTokens !== undefined
            ? { cacheReadTokens: usage.cacheReadTokens }
            : {}),
          ...(usage.cacheWriteTokens !== undefined
            ? { cacheWriteTokens: usage.cacheWriteTokens }
            : {}),
          ...(usage.reasoningTokens !== undefined
            ? { reasoningTokens: usage.reasoningTokens }
            : {}),
          ...(usage.totalTokens !== undefined
            ? { totalTokens: usage.totalTokens }
            : {}),
          ...cost,
          ...(usage.estimatedCostUsd !== undefined
            ? { estimatedCostUsd: usage.estimatedCostUsd }
            : {}),
          ...(usage.billedCostUsd !== undefined
            ? { billedCostUsd: usage.billedCostUsd }
            : {}),
          ...(usage.currency ? { currency: usage.currency } : {}),
          ...(usage.pricingCatalogVersion
            ? { pricingCatalogVersion: usage.pricingCatalogVersion }
            : {}),
          ...(diagnostics
            ? {
                promptChars: diagnostics.promptChars,
                attempts: diagnostics.attempts,
                toolCalls: diagnostics.toolCalls,
                toolOutputBytes: diagnostics.toolOutputBytes,
                ...(diagnostics.graphjinAgent
                  ? {
                      graphjinAgentCalls: diagnostics.graphjinAgent.calls,
                      graphjinAgentModelCalls: diagnostics.graphjinAgent.modelCalls,
                      graphjinAgentDurationMs: diagnostics.graphjinAgent.durationMs,
                      graphjinAgentFailures: diagnostics.graphjinAgent.failures,
                    }
                  : {}),
              }
            : {}),
        },
      };
    },
    score({ case: evalCase, oracle: rawOracle, execution }) {
      const oracle = rawOracle as AdventureWorksOracle;
      const output = outputOf(execution);
      const parsed =
        parseHeadline(output.headlineMetric) ?? output.chartData[0]?.v ?? null;
      const checks = evalCase.assertions.map((assertion) => {
        if (assertion.kind === "numeric.relative-error") {
          const tolerance = Number(assertion.params?.max_relative_error ?? 0.01);
          const relativeError =
            parsed == null
              ? Number.POSITIVE_INFINITY
              : oracle.expectedValue === 0
                ? Math.abs(parsed)
                : Math.abs(parsed - oracle.expectedValue) / Math.abs(oracle.expectedValue);
          return {
            assertionId: assertion.id,
            dimension: assertion.dimension,
            passed: relativeError <= tolerance,
            score: Number.isFinite(relativeError)
              ? Math.max(0, 1 - relativeError / Math.max(tolerance, 1e-9))
              : 0,
            gate: assertion.gate,
            diagnostic: `relative_error=${Number.isFinite(relativeError) ? relativeError : "unavailable"}`,
          };
        }
        if (assertion.kind === "numeric.baseline-relative-error") {
          const tolerance = Number(assertion.params?.max_relative_error ?? 0.01);
          const actual = output.baselineValue ?? output.chartData[0]?.t;
          const relativeError =
            actual == null
              ? Number.POSITIVE_INFINITY
              : oracle.baselineValue === 0
                ? Math.abs(actual)
                : Math.abs(actual - oracle.baselineValue) /
                  Math.abs(oracle.baselineValue);
          return {
            assertionId: assertion.id,
            dimension: assertion.dimension,
            passed: relativeError <= tolerance,
            score: Number.isFinite(relativeError)
              ? Math.max(0, 1 - relativeError / Math.max(tolerance, 1e-9))
              : 0,
            gate: assertion.gate,
            diagnostic: `relative_error=${Number.isFinite(relativeError) ? relativeError : "unavailable"}`,
          };
        }
        if (assertion.kind === "time-window.exact") {
          const expectedGrain = String(assertion.params?.grain ?? "");
          const passed =
            output.timeWindow.grain === expectedGrain &&
            output.timeWindow.start === oracle.startDate &&
            output.timeWindow.end === oracle.anchorDate;
          return {
            assertionId: assertion.id,
            dimension: assertion.dimension,
            passed,
            gate: assertion.gate,
            diagnostic: `expected=${expectedGrain}:${oracle.startDate}:${oracle.anchorDate}`,
          };
        }
        if (assertion.kind === "metric.contract") {
          const validationError = validateResult(output);
          return {
            assertionId: assertion.id,
            dimension: assertion.dimension,
            passed: validationError === null,
            gate: assertion.gate,
            ...(validationError ? { diagnostic: validationError } : {}),
          };
        }
        if (assertion.kind === "dimension.contains") {
          const expected = oracle.expectedDimension?.toLocaleLowerCase();
          const passed = Boolean(
            expected && JSON.stringify(output).toLocaleLowerCase().includes(expected),
          );
          return {
            assertionId: assertion.id,
            dimension: assertion.dimension,
            passed,
            gate: assertion.gate,
            diagnostic: `expected_dimension=${oracle.expectedDimension ?? "missing"}`,
          };
        }
        if (assertion.kind === "duration.under") {
          const actual = Number(execution.measurements?.wallDurationMs);
          const maximum = Number(assertion.params?.max_ms ?? 600_000);
          return {
            assertionId: assertion.id,
            dimension: assertion.dimension,
            passed: Number.isFinite(actual) && actual <= maximum,
            score: Number.isFinite(actual) ? Math.max(0, Math.min(1, maximum / Math.max(actual, 1))) : 0,
            gate: assertion.gate,
            diagnostic: `duration_ms=${actual}`,
          };
        }
        throw new Error(`unknown metric assertion ${assertion.kind}`);
      });
      return createScore({
        scorerId: "adventureworks.metric",
        scorerVersion: "2.0.0",
        scorerDefinition: {
          classification: "natural question to card metadata via production classifier",
          numeric: "relative error against host SQL",
          baseline: "baselineValue (else the first chart target) relative error against host SQL",
          timeWindow: "exact grain/start/end",
          contract: "OpenNeko metric result contract",
        },
        checks,
      });
    },
    async close() {
      await pool?.end();
      if (!process.env.OPENNEKO_EVAL_ORG_ID) {
        for (const runtime of runtimes.values()) {
          await deleteTestOrg(runtime.orgId).catch(() => {});
        }
      }
      await shutdownAgentBroker();
      await reconnectPool();
      if (isolatedConfigRoot) {
        await rm(isolatedConfigRoot, { recursive: true, force: true });
      }
      for (const [name, value] of originalEnv) {
        if (value === undefined) delete process.env[name];
        else process.env[name] = value;
      }
    },
  };
}
