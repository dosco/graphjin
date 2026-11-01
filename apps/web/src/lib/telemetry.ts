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

type Environment = Readonly<Record<string, string | undefined>>;

let runtime: OtlpTelemetryRuntime | undefined;
let initializationAttempted = false;

export function isWebTelemetryEnabled(
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

/** Lazily initializes one exporter for the Next.js server process. */
export function initializeWebTelemetry(
  environment: Environment = process.env,
): boolean {
  if (initializationAttempted) return Boolean(runtime);
  initializationAttempted = true;
  if (!isWebTelemetryEnabled(environment)) return false;
  try {
    runtime = createOtlpTelemetryRuntime({
      serviceName: environment.OTEL_SERVICE_NAME ?? "openneko-web",
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
    // Observability is deliberately fail-open for the product path.
    console.warn("[telemetry] OpenTelemetry initialization failed; continuing disabled");
    runtime = undefined;
    return false;
  }
}

export function createWebHarnessObserver(runId: string): {
  observer: HarnessObserver;
  snapshot(): HarnessRunSummary;
} {
  initializeWebTelemetry();
  const summary = new HarnessRunSummaryAccumulator(runId);
  const sinks: ObservationSink[] = [summary];
  if (runtime) sinks.push(runtime.sink);
  return {
    observer: createHarnessObserver({ runId, sinks }),
    snapshot: () => summary.snapshot(),
  };
}

function isTruthy(value: string | undefined): boolean {
  return /^(1|true|yes|on)$/i.test(value?.trim() ?? "");
}
