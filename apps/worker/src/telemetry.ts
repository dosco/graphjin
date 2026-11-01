import {
  HarnessRunSummaryAccumulator,
  createHarnessObserver,
  type HarnessObserver,
  type HarnessRunSummary,
  type ObservationSink,
} from "@neko/telemetry";
import {
  createOtlpTelemetryRuntime,
  type OtlpTelemetryRuntime,
} from "@neko/telemetry/node";
import { db, eq, processing_job, sql, workflow_run } from "@neko/db";

type Environment = Readonly<Record<string, string | undefined>>;

let runtime: OtlpTelemetryRuntime | undefined;
let initializationAttempted = false;

export function isWorkerTelemetryEnabled(
  environment: Environment = process.env,
): boolean {
  if (isTruthy(environment.OTEL_SDK_DISABLED)) return false;
  const explicit = environment.OPENNEKO_OTEL_ENABLED;
  if (explicit !== undefined) return isTruthy(explicit);
  return Boolean(
    environment.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT ??
      environment.OTEL_EXPORTER_OTLP_ENDPOINT,
  );
}

/** Initialize once per worker process. Exporter configuration follows OTel env vars. */
export function initializeWorkerTelemetry(
  environment: Environment = process.env,
): boolean {
  if (initializationAttempted) return Boolean(runtime);
  initializationAttempted = true;
  if (!isWorkerTelemetryEnabled(environment)) return false;
  try {
    runtime = createOtlpTelemetryRuntime({
      serviceName: environment.OTEL_SERVICE_NAME ?? "openneko-worker",
      ...(environment.OPENNEKO_VERSION
        ? { serviceVersion: environment.OPENNEKO_VERSION }
        : {}),
      ...(environment.OPENNEKO_DEPLOYMENT_ENVIRONMENT
        ? {
            deploymentEnvironment:
              environment.OPENNEKO_DEPLOYMENT_ENVIRONMENT,
          }
        : {}),
    });
    return true;
  } catch {
    // Instrumentation failure must not prevent the worker from starting.
    console.warn("[telemetry] OpenTelemetry initialization failed; continuing disabled");
    runtime = undefined;
    return false;
  }
}

export function createWorkerHarnessObserver(runId: string): {
  observer: HarnessObserver;
  snapshot(): HarnessRunSummary;
} {
  const summary = new HarnessRunSummaryAccumulator(runId);
  const sinks: ObservationSink[] = [summary];
  if (runtime) sinks.push(runtime.sink);
  return {
    observer: createHarnessObserver({ runId, sinks }),
    snapshot: () => summary.snapshot(),
  };
}

/** Persist the safe summary in the existing processing-job client surface. */
export async function persistProcessingJobTelemetry(
  processingJobId: string,
  summary: HarnessRunSummary,
): Promise<void> {
  try {
    await db()
      .update(processing_job)
      .set({
        result: sql`coalesce(${processing_job.result}, '{}'::jsonb) || ${JSON.stringify({ telemetry: summary })}::jsonb`,
        updated_at: new Date(),
      })
      .where(eq(processing_job.id, processingJobId));
  } catch {
    // Summary persistence must not affect the product-path outcome.
    console.warn(`[telemetry] failed to persist processing-job summary ${processingJobId}`);
  }
}

/** Canonical workflow-run summary used by history, API status, and budgets. */
export async function persistWorkflowRunTelemetry(
  workflowRunId: string,
  summary: HarnessRunSummary,
): Promise<void> {
  try {
    await db()
      .update(workflow_run)
      .set({ telemetry_summary: summary, updated_at: new Date() })
      .where(eq(workflow_run.id, workflowRunId));
  } catch {
    console.warn(
      `[telemetry] failed to persist workflow-run summary ${workflowRunId}`,
    );
  }
}

export async function shutdownWorkerTelemetry(): Promise<void> {
  const current = runtime;
  runtime = undefined;
  if (!current) return;
  try {
    // The sink first closes any incomplete spans, then shuts down the provider.
    await current.sink.shutdown();
  } catch {
    console.warn("[telemetry] OpenTelemetry shutdown failed");
  }
}

function isTruthy(value: string | undefined): boolean {
  return /^(1|true|yes|on)$/i.test(value?.trim() ?? "");
}
