import { bindStartupRun, startupPhase, withStartupTrace } from "@neko/telemetry/startup";
import {
  and,
  data_source,
  db,
  desc,
  eq,
  metric,
  metric_snapshot,
  processing_job,
} from "@neko/db";
import { updateProgress } from "../progress.js";
import {
  ensureHostConfigProvisioned,
  runMetricAgent,
  type AgentTokenUsage,
  type MetricAgentDiagnostics,
  type MetricAgentInput,
  type MetricAgentResult,
} from "@neko/llm";
import {
  createWorkerHarnessObserver,
  persistProcessingJobTelemetry,
} from "../telemetry.js";
import { observeSafely } from "@neko/telemetry";
import { recordUsageSpend, releaseSpendReservation, reserveSpend } from "@neko/llm/spend";
import { graphjinQuery, mintGraphjinToken, packArtifactSource } from "@neko/llm/graphjin";
import {
  buildSavedQueryVariables,
  mapSavedQueryMetric,
  parseMetricDefinition,
} from "./deterministic-metric.js";

/**
 * metric_refresh job — runs the metric agent for ONE card and writes the
 * result to metric_snapshot. Two entry paths:
 *
 *   1. Bootstrap card: trigger_payload.metricId → load card from metric table.
 *   2. Ad-hoc chat question: trigger_payload.question + inline metadata
 *      (slug, title, why, chartHint, role). Creates a metric row with
 *      source='chat', active=false, then writes the snapshot against it.
 */
export function runMetricRefresh(jobId: string, orgId: string) {
  return withStartupTrace({ runId: jobId, jobId, rootOperationId: `metric:${jobId}` }, () => startupPhase("metric.refresh", () => runMetricRefreshTraced(jobId, orgId)));
}

async function runMetricRefreshTraced(jobId: string, orgId: string) {
  await startupPhase("job.progress", async () => updateProgress(jobId, "Loading card"));

  const jobRows = await startupPhase("metric.db_read", async () => db()
    .select({ trigger_payload: processing_job.trigger_payload })
    .from(processing_job)
    .where(and(eq(processing_job.id, jobId), eq(processing_job.org_id, orgId)))
    .limit(1));
  const payload = jobRows[0]?.trigger_payload as
    | {
        metricId?: string;
        question?: string;
        slug?: string;
        title?: string;
        why?: string;
        chartHint?: string;
        role?: string;
        classification?: {
          durationMs: number;
          usage: AgentTokenUsage;
          provider?: string;
          model?: string;
        };
      }
    | null
    | undefined;
  if (!payload) {
    throw new Error(`metric_refresh job ${jobId} has no trigger_payload`);
  }

  let metricRowId: string;
  let input: MetricAgentInput;
  let savedQueryDefinition: Record<string, unknown> | null = null;
  let savedQueryDefinitionHash: string | null = null;

  if (payload.metricId) {
    // Path 1: bootstrap card — load from metric table.
    const cards = await startupPhase("metric.db_read", async () => db()
      .select({
        id: metric.id,
        role: metric.role,
        slug: metric.slug,
        title: metric.title,
        why: metric.why,
        chart_hint: metric.chart_hint,
        execution_mode: metric.execution_mode,
        definition_json: metric.definition_json,
        definition_hash: metric.definition_hash,
      })
      .from(metric)
      .where(and(eq(metric.id, payload.metricId!), eq(metric.org_id, orgId)))
      .limit(1));
    const card = cards[0];
    if (!card) throw new Error(`metric ${payload.metricId} not found`);
    metricRowId = card.id;
    if (card.execution_mode === "saved_query") {
      savedQueryDefinition = card.definition_json;
      savedQueryDefinitionHash = card.definition_hash;
      if (!savedQueryDefinition || !savedQueryDefinitionHash) {
        throw new Error(`saved-query metric ${payload.metricId} has no validated definition`);
      }
    }
    input = {
      orgId,
      role: card.role as MetricAgentInput["role"],
      slug: card.slug,
      title: card.title,
      why: card.why ?? card.title,
      chartHint: (card.chart_hint ?? "line") as MetricAgentInput["chartHint"],
    };
  } else if (payload.question) {
    // Path 2: ad-hoc chat question.
    const slug = payload.slug || `chat-${Date.now()}`;
    const role = (payload.role ?? "CEO") as MetricAgentInput["role"];
    const title = payload.title || payload.question.slice(0, 60);
    const why = payload.why || payload.question;
    const chartHint = (payload.chartHint ?? "bar") as MetricAgentInput["chartHint"];

    const existingRows = await startupPhase("metric.db_read", async () => db()
      .select({ id: metric.id })
      .from(metric)
      .where(
        and(
          eq(metric.org_id, orgId),
          eq(metric.role, role),
          eq(metric.slug, slug),
        ),
      )
      .limit(1));
    const existingId = existingRows[0]?.id;

    if (existingId) {
      metricRowId = existingId;
      // Re-link to the current job + reset refresh status. The status endpoint
      // locates the snapshot via metric.created_by_job, so without this update
      // a rerun (or any re-ask that hits the same slug) leaves the row pointing
      // at the first job's id, and the status route returns payload=null. The
      // last_refresh_status reset moves the card back to "pending" so the
      // dashboard re-skeletons while the new run is in flight.
      await startupPhase("metric.persist", async () => db()
        .update(metric)
        .set({
          created_by_job: jobId,
          last_refresh_status: "pending",
          last_refresh_error: null,
          last_refresh_job_id: jobId,
        })
        .where(eq(metric.id, existingId)));
    } else {
      const ins = await startupPhase("metric.persist", async () => db()
        .insert(metric)
        .values({
          org_id: orgId,
          role,
          slug,
          source: "chat",
          title,
          why,
          chart_hint: chartHint,
          active: false,
          created_by_job: jobId,
          last_refresh_status: "pending",
          last_refresh_job_id: jobId,
        })
        .returning({ id: metric.id }));
      const newId = ins[0]?.id;
      if (!newId) throw new Error("failed to insert chat metric row");
      metricRowId = newId;
    }

    input = {
      orgId,
      question: payload.question,
      role,
      slug,
      title,
      why,
      chartHint,
    };
  } else {
    throw new Error(
      `metric_refresh job ${jobId} has neither metricId nor question in trigger_payload`,
    );
  }

  if (savedQueryDefinition && savedQueryDefinitionHash) {
    await runDeterministicMetricRefresh({
      jobId,
      orgId,
      metricId: metricRowId,
      slug: input.slug,
      definition: savedQueryDefinition,
      definitionHash: savedQueryDefinitionHash,
    });
    return;
  }

  await startupPhase("job.progress", async () => updateProgress(jobId, "Running agent"));
  // Stamp metric.last_refresh_status on BOTH the success and failure paths
  // so the briefing API can render a Retry button without joining
  // processing_job (which only knows the job id, not the metric id). The
  // outer makeHandler still marks processing_job.status='failed' on rethrow.
  let runTelemetry:
    | ReturnType<typeof createWorkerHarnessObserver>
    | undefined;
  const telemetryStartedAt = Date.now();
  let telemetryFailure: unknown;
  let spendReservationId: string | undefined;
  let metricDiagnostics: MetricAgentDiagnostics | undefined;
  try {
    let result: MetricAgentResult;
    runTelemetry = createWorkerHarnessObserver(jobId);
    await bindStartupRun(jobId, runTelemetry.observer, `metric:${jobId}`);
    if (payload.classification) {
      await observeSafely(runTelemetry.observer, {
        kind: "model.request",
        operationId: `metric:${jobId}:classification`,
        parentOperationId: `metric:${jobId}`,
        attributes: {
          "openneko.model.scope": "classification",
          ...(payload.classification.provider
            ? { "gen_ai.provider.name": payload.classification.provider }
            : {}),
          ...(payload.classification.model
            ? { "gen_ai.request.model": payload.classification.model }
            : {}),
        },
      });
      await observeSafely(runTelemetry.observer, {
        kind: "model.response",
        operationId: `metric:${jobId}:classification`,
        parentOperationId: `metric:${jobId}`,
        status: "ok",
        attributes: {
          "openneko.model.scope": "classification",
          ...(payload.classification.provider
            ? { "gen_ai.provider.name": payload.classification.provider }
            : {}),
          ...(payload.classification.model
            ? { "gen_ai.response.model": payload.classification.model }
            : {}),
        },
        measurements: {
          ...payload.classification.usage,
          durationMs: payload.classification.durationMs,
        },
      });
    }
    await startupPhase("config.provision", async () => ensureHostConfigProvisioned(orgId));
    spendReservationId = (await reserveSpend({ orgId, source: "metric" })).reservationId;
    result = await startupPhase("metric.agent", async () => runMetricAgent({
      ...input,
      jobId,
      onDiagnostics: (diagnostics) => {
        metricDiagnostics = diagnostics;
        input.onDiagnostics?.(diagnostics);
      },
      observer: runTelemetry!.observer,
      observationAttributes: {
        "openneko.job.kind": "metric_refresh",
      },
    }));

    const validationError = validateResult(result);
    await observeSafely(runTelemetry.observer, {
      kind: "validation.result",
      operationId: `metric:${jobId}:persisted-contract`,
      parentOperationId: `metric:${jobId}`,
      status: validationError ? "error" : "ok",
      ...(validationError ? { errorType: "metric_result_invalid" } : {}),
      attributes: { "openneko.validation.contract": "metric-persistence" },
    });
    if (validationError) {
      throw new Error(`metric agent output invalid: ${validationError}`);
    }

    await startupPhase("job.progress", async () => updateProgress(jobId, "Saving snapshot"));

    await startupPhase("metric.persist", async () => db().insert(metric_snapshot).values({
      metric_id: metricRowId,
      status: result.mood,
      payload: result,
    }));

    await startupPhase("metric.persist", async () => db()
      .update(metric)
      .set({
        last_refresh_status: "ok",
        last_refresh_error: null,
        last_refresh_job_id: jobId,
        updated_at: new Date(),
      })
      .where(eq(metric.id, metricRowId)));

    console.log(
      `[metric_refresh] org=${orgId} metric=${input.slug} mood=${result.mood} "${result.headlineMetric}"`,
    );
  } catch (e) {
    telemetryFailure = e;
    if (runTelemetry && runTelemetry.snapshot().status !== "failed") {
      await observeSafely(runTelemetry.observer, {
        kind: "error",
        operationId: `metric:${jobId}:handler-error`,
        parentOperationId: `metric:${jobId}`,
        status: "error",
        errorType: e instanceof Error ? e.name : "unknown",
      });
    }
    const msg = e instanceof Error ? e.message : String(e);
    await startupPhase("metric.persist", async () => db()
      .update(metric)
      .set({
        last_refresh_status: "failed",
        last_refresh_error: msg.slice(0, 500),
        last_refresh_job_id: jobId,
        updated_at: new Date(),
      })
      .where(eq(metric.id, metricRowId)));
    throw e;
  } finally {
    if (spendReservationId) {
      for (const usage of [metricDiagnostics?.outerUsage, metricDiagnostics?.innerUsage]) {
        if (!usage) continue;
        await recordUsageSpend({ orgId, reservationId: spendReservationId, usage }).catch((error) => {
          console.error(`[metric_refresh] could not record spend for job=${jobId}: ${error instanceof Error ? error.message : error}`);
        });
      }
      await releaseSpendReservation(spendReservationId).catch(() => undefined);
    }
    if (runTelemetry) {
      // Structured and content-free: safe for production logs and collectors.
      if (!runTelemetry.snapshot().finishedAt) {
        await observeSafely(runTelemetry.observer, {
          kind: "run.end",
          operationId: `metric:${jobId}`,
          status: telemetryFailure ? "error" : "ok",
          ...(telemetryFailure
            ? {
                errorType:
                  telemetryFailure instanceof Error
                    ? telemetryFailure.name
                    : "unknown",
              }
            : {}),
          attributes: {
            "openneko.outcome": telemetryFailure ? "failed" : "completed",
          },
          measurements: {
            durationMs: Date.now() - telemetryStartedAt,
            coverage: "unavailable",
          },
        });
      }
      const summary = runTelemetry.snapshot();
      await persistProcessingJobTelemetry(jobId, summary);
      console.log(
        `[metric_refresh.telemetry] ${JSON.stringify(summary)}`,
      );
    }
  }
}

function graphjinEndpoint(url: string): string {
  const clean = url.replace(/\/+$/, "");
  return clean.endsWith("/api/v1/graphql") ? clean : `${clean}/api/v1/graphql`;
}

async function runDeterministicMetricRefresh(input: {
  jobId: string;
  orgId: string;
  metricId: string;
  slug: string;
  definition: Record<string, unknown>;
  definitionHash: string;
}): Promise<void> {
  const startedAt = Date.now();
  const telemetry = createWorkerHarnessObserver(input.jobId);
  let failure: unknown;
  await observeSafely(telemetry.observer, {
    kind: "run.start",
    operationId: `metric:${input.jobId}`,
    attributes: {
      "openneko.job.kind": "metric_refresh",
      "openneko.metric.execution_mode": "saved_query",
    },
  });
  await bindStartupRun(input.jobId, telemetry.observer, `metric:${input.jobId}`);
  try {
    const definition = parseMetricDefinition(input.definition);
    await startupPhase("job.progress", async () => updateProgress(input.jobId, "Running reviewed saved query"));
    const boundSource = await startupPhase("metric.source", async () => packArtifactSource(input.orgId, "metric", input.metricId));
    const [fallbackSource] = boundSource ? [] : await startupPhase("metric.db_read", async () => db()
      .select({
        graphqlUrl: data_source.graphql_url,
        authMode: data_source.auth_mode,
      })
      .from(data_source)
      .where(and(eq(data_source.org_id, input.orgId), eq(data_source.enabled, true)))
      .orderBy(desc(data_source.is_default), data_source.created_at)
      .limit(1));
    const source = boundSource ?? fallbackSource;
    if (!source?.graphqlUrl) throw new Error("saved-query metric has no enabled GraphJin source");

    const [previous] = await startupPhase("metric.db_read", async () => db()
      .select({ value: metric_snapshot.value })
      .from(metric_snapshot)
      .where(eq(metric_snapshot.metric_id, input.metricId))
      .orderBy(desc(metric_snapshot.captured_at))
      .limit(1));
    const previousValue = previous?.value === null || previous?.value === undefined
      ? null
      : Number(previous.value);
    const baseline = previousValue !== null && Number.isFinite(previousValue) ? previousValue : null;
    const now = new Date();
    const response = await startupPhase("metric.saved_query", async () => graphjinQuery<Record<string, unknown>>({
      baseUrl: graphjinEndpoint(source.graphqlUrl),
      query: definition.execution.document,
      variables: buildSavedQueryVariables(input.definition, now),
      role: "service",
      ...(source.authMode === "jwt"
        ? {
            headers: {
              authorization: `Bearer ${mintGraphjinToken({
                orgId: input.orgId,
                userId: null,
                role: "service",
              })}`,
            },
          }
        : {}),
      signal: AbortSignal.timeout(30_000),
    }));
    if (response.errors?.length) {
      throw new Error(`saved-query metric failed: ${response.errors.map((error) => error.message).join("; ")}`);
    }
    if (!response.data) throw new Error("saved-query metric returned no data");
    const mapped = mapSavedQueryMetric({
      definition: input.definition,
      data: response.data,
      baseline,
      now,
    });
    const validationError = validateResult(mapped.result);
    await observeSafely(telemetry.observer, {
      kind: "validation.result",
      operationId: `metric:${input.jobId}:persisted-contract`,
      parentOperationId: `metric:${input.jobId}`,
      status: validationError ? "error" : "ok",
      ...(validationError ? { errorType: "metric_result_invalid" } : {}),
      attributes: { "openneko.validation.contract": "deterministic-metric-persistence" },
    });
    if (validationError) throw new Error(`saved-query metric output invalid: ${validationError}`);

    await startupPhase("job.progress", async () => updateProgress(input.jobId, "Saving deterministic snapshot"));
    const durationMs = Date.now() - startedAt;
    await startupPhase("metric.persist", async () => db().insert(metric_snapshot).values({
      metric_id: input.metricId,
      value: mapped.value === null ? null : String(mapped.value),
      value_json: mapped.valueJson,
      baseline: mapped.baseline === null ? null : String(mapped.baseline),
      delta_pct: mapped.deltaPct === null ? null : String(mapped.deltaPct),
      status: mapped.result.mood,
      payload: mapped.result,
      definition_hash: input.definitionHash,
      source_freshness_at: mapped.sourceFreshnessAt,
      duration_ms: durationMs,
      error_class: null,
    }));
    await startupPhase("metric.persist", async () => db()
      .update(metric)
      .set({
        last_refresh_status: "ok",
        last_refresh_error: null,
        last_refresh_job_id: input.jobId,
        updated_at: new Date(),
      })
      .where(eq(metric.id, input.metricId)));
    console.log(
      `[metric_refresh] org=${input.orgId} metric=${input.slug} mode=saved_query mood=${mapped.result.mood} durationMs=${durationMs}`,
    );
  } catch (error) {
    failure = error;
    const message = error instanceof Error ? error.message : String(error);
    await startupPhase("metric.persist", async () => db()
      .update(metric)
      .set({
        last_refresh_status: "failed",
        last_refresh_error: message.slice(0, 500),
        last_refresh_job_id: input.jobId,
        updated_at: new Date(),
      })
      .where(eq(metric.id, input.metricId)));
    await observeSafely(telemetry.observer, {
      kind: "error",
      operationId: `metric:${input.jobId}:saved-query-error`,
      parentOperationId: `metric:${input.jobId}`,
      status: "error",
      errorType: error instanceof Error ? error.name : "unknown",
    });
    throw error;
  } finally {
    await observeSafely(telemetry.observer, {
      kind: "run.end",
      operationId: `metric:${input.jobId}`,
      status: failure ? "error" : "ok",
      ...(failure ? { errorType: failure instanceof Error ? failure.name : "unknown" } : {}),
      attributes: {
        "openneko.outcome": failure ? "failed" : "completed",
        "openneko.metric.execution_mode": "saved_query",
      },
      measurements: {
        durationMs: Date.now() - startedAt,
        coverage: "unavailable",
      },
    });
    await persistProcessingJobTelemetry(input.jobId, telemetry.snapshot());
  }
}

const VALID_MOODS = new Set(["good", "watch", "bad"]);
const VALID_CHART_TYPES = new Set(["kpi", "line", "bar", "donut", "area"]);
// Headline strings that mean "the agent failed to fetch real data and is
// narrating the failure." Treat them as a job failure — the worker marks
// the job failed and the briefing UI surfaces a Retry button for the card.
// Without this gate, the result row goes into metric_snapshot with
// `headlineMetric: "Error"` and the dashboard renders the error narrative
// as if it were a metric.
const FAIL_SENTINELS = new Set([
  "error",
  "errors",
  "n/a",
  "na",
  "unavailable",
  "data unavailable",
  "data unavilable", // tolerate the agent's typo seen in the wild
  "no data",
  "null",
  "undefined",
  "—",
  "-",
  "?",
  "tbd",
]);
const ALL_PUNCT_RE = /^[\s\-—?_.]+$/;
const VALID_TIME_GRAINS = new Set([
  "day",
  "week",
  "month",
  "quarter",
  "year",
  "all_time",
  "snapshot",
]);
const ISO_DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

export function validateResult(r: MetricAgentResult): string | null {
  if (!r.headlineMetric || !r.headlineMetric.trim()) return "empty headlineMetric";
  const headlineNorm = r.headlineMetric.trim().toLowerCase();
  if (FAIL_SENTINELS.has(headlineNorm) || ALL_PUNCT_RE.test(headlineNorm)) {
    return `agent emitted error sentinel as headlineMetric: "${r.headlineMetric}"`;
  }
  if (!r.headlineLabel) return "empty headlineLabel";
  if (!r.insightText) return "empty insightText";
  if (!VALID_MOODS.has(r.mood)) return `invalid mood: ${r.mood}`;
  if (!VALID_CHART_TYPES.has(r.chartType)) return `invalid chartType: ${r.chartType}`;
  if (!Array.isArray(r.chartData) || r.chartData.length === 0) return "empty chartData";
  for (const item of r.chartData) {
    if (typeof item.v !== "number" || isNaN(item.v)) return "chartData item has non-numeric v";
    if (!item.d) return "chartData item missing d label";
  }
  if (r.chartType === "kpi" && r.chartData.length !== 1) {
    return `kpi chartType requires exactly 1 chartData item, got ${r.chartData.length}`;
  }
  if (r.chartType === "kpi" && r.chartData[0].t == null) {
    return "kpi chartType requires a baseline (t) value";
  }

  // Time window — required on every snapshot.
  if (!r.timeWindow) return "missing timeWindow";
  if (!VALID_TIME_GRAINS.has(r.timeWindow.grain)) {
    return `invalid timeWindow.grain: ${r.timeWindow.grain}`;
  }
  if (!r.timeWindow.label) return "empty timeWindow.label";
  if (r.timeWindow.grain !== "all_time") {
    if (!r.timeWindow.start || !ISO_DATE_RE.test(r.timeWindow.start)) {
      return `timeWindow.start must be yyyy-mm-dd (got "${r.timeWindow.start}")`;
    }
    if (!r.timeWindow.end || !ISO_DATE_RE.test(r.timeWindow.end)) {
      return `timeWindow.end must be yyyy-mm-dd (got "${r.timeWindow.end}")`;
    }
  }
  return null;
}
