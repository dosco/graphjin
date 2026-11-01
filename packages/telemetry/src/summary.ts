import type {
  HarnessObservation,
  NormalizedUsage,
  ObservationSink,
} from "./types";

export type HarnessRunSummary = {
  schemaVersion: "openneko.harness-run-summary/v1";
  runId: string;
  traceId?: string;
  startedAt?: string;
  firstOutputAt?: string;
  finishedAt?: string;
  status: "running" | "completed" | "failed" | "cancelled";
  errorType?: string;
  errorCode?: string;
  errorMessage?: string;
  backend?: string;
  provider?: string;
  requestedModel?: string;
  resolvedModel?: string;
  dataPath?: string;
  productPath?: string;
  durations: {
    wallMs?: number;
    queueMs?: number;
    firstOutputMs?: number;
  };
  phases?: Array<{ name: string; durationMs: number; ok: boolean }>;
  io?: {
    inputBytes?: number;
    outputBytes?: number;
  };
  counts: {
    inference: number;
    tools: number;
    delegations: number;
    retries: number;
    validations: number;
  };
  batch?: {
    acceptedRows?: number;
    processedRows?: number;
    finalRows?: number;
    chunkCount?: number;
    artifactBytes?: number;
  };
  usage: NormalizedUsage;
  telemetryComplete: boolean;
};

function asString(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function add(left: number | undefined, right: number | undefined): number | undefined {
  if (left === undefined && right === undefined) return undefined;
  return (left ?? 0) + (right ?? 0);
}

export class HarnessRunSummaryAccumulator implements ObservationSink {
  private readonly value: HarnessRunSummary;
  private sawUsageValue = false;
  private sawUsageGap = false;

  constructor(runId: string) {
    this.value = {
      schemaVersion: "openneko.harness-run-summary/v1",
      runId,
      status: "running",
      durations: {},
      counts: {
        inference: 0,
        tools: 0,
        delegations: 0,
        retries: 0,
        validations: 0,
      },
      usage: { coverage: "unavailable", missingReasons: ["no model usage observed"] },
      telemetryComplete: false,
    };
  }

  emit(observation: HarnessObservation): void {
    const attrs = observation.attributes;
    const phase = attrs["openneko.stage"];
    if (observation.kind === "stage.end" && typeof phase === "string" && phase.startsWith("startup.") && observation.measurements?.durationMs !== undefined) {
      this.value.phases ??= [];
      if (this.value.phases.length < 128) this.value.phases.push({ name: phase.slice(8), durationMs: observation.measurements.durationMs, ok: observation.status !== "error" });
    }
    this.value.durations.queueMs ??= observation.measurements?.queueDurationMs;
    this.value.traceId ??= observation.traceId;
    this.value.backend ??= asString(attrs["openneko.backend"]);
    this.value.provider ??= asString(attrs["gen_ai.provider.name"]);
    this.value.requestedModel ??= asString(attrs["gen_ai.request.model"]);
    this.value.resolvedModel ??= asString(attrs["gen_ai.response.model"]);
    this.value.dataPath ??= asString(attrs["openneko.data.path"]);
    this.value.productPath ??= asString(attrs["openneko.product.path"]);

    if (observation.kind === "run.start") this.value.startedAt = observation.timestamp;
    if (observation.kind === "run.first_output") {
      this.value.firstOutputAt = observation.timestamp;
      this.value.durations.firstOutputMs = observation.measurements?.firstOutputMs;
    }
    if (observation.kind === "model.request") this.value.counts.inference += 1;
    if (observation.kind === "tool.start") this.value.counts.tools += 1;
    if (observation.kind === "delegation.start") this.value.counts.delegations += 1;
    if (observation.kind === "retry") this.value.counts.retries += 1;
    if (observation.kind === "validation.result") this.value.counts.validations += 1;

    if (
      observation.status === "error" &&
      (observation.kind === "error" ||
        observation.kind === "validation.result" ||
        observation.kind === "output.contract")
    ) {
      this.value.status = "failed";
      this.recordError(observation);
    }

    const measurements = observation.measurements;
    if (measurements) {
      if (
        measurements.inputBytes !== undefined ||
        measurements.outputBytes !== undefined
      ) {
        this.value.io = {
          inputBytes: add(
            this.value.io?.inputBytes,
            measurements.inputBytes,
          ),
          outputBytes: add(
            this.value.io?.outputBytes,
            measurements.outputBytes,
          ),
        };
      }
      if (
        measurements.acceptedRows !== undefined ||
        measurements.processedRows !== undefined ||
        measurements.finalRows !== undefined ||
        measurements.chunkCount !== undefined ||
        measurements.artifactBytes !== undefined
      ) {
        this.value.batch = {
          ...(this.value.batch ?? {}),
          ...(measurements.acceptedRows !== undefined
            ? { acceptedRows: measurements.acceptedRows }
            : {}),
          ...(measurements.processedRows !== undefined
            ? { processedRows: measurements.processedRows }
            : {}),
          ...(measurements.finalRows !== undefined
            ? { finalRows: measurements.finalRows }
            : {}),
          ...(measurements.chunkCount !== undefined
            ? { chunkCount: measurements.chunkCount }
            : {}),
          ...(measurements.artifactBytes !== undefined
            ? { artifactBytes: measurements.artifactBytes }
            : {}),
        };
      }
      const current = this.value.usage;
      const hasUsageValue = [
        measurements.inputTokens,
        measurements.outputTokens,
        measurements.cacheReadTokens,
        measurements.cacheWriteTokens,
        measurements.reasoningTokens,
        measurements.totalTokens,
        measurements.estimatedCostUsd,
        measurements.billedCostUsd,
      ].some((item) => item !== undefined);
      this.sawUsageValue ||= hasUsageValue;
      this.sawUsageGap ||=
        measurements.coverage !== "complete" &&
        (hasUsageValue || Boolean(measurements.missingReasons?.length));
      this.value.usage = {
        inputTokens: add(current.inputTokens, measurements.inputTokens),
        outputTokens: add(current.outputTokens, measurements.outputTokens),
        cacheReadTokens: add(current.cacheReadTokens, measurements.cacheReadTokens),
        cacheWriteTokens: add(current.cacheWriteTokens, measurements.cacheWriteTokens),
        reasoningTokens: add(current.reasoningTokens, measurements.reasoningTokens),
        totalTokens: add(current.totalTokens, measurements.totalTokens),
        estimatedCostUsd: add(
          current.estimatedCostUsd,
          measurements.estimatedCostUsd,
        ),
        billedCostUsd: add(current.billedCostUsd, measurements.billedCostUsd),
        ...(measurements.currency ? { currency: measurements.currency } : {}),
        ...(measurements.pricingCatalogVersion
          ? { pricingCatalogVersion: measurements.pricingCatalogVersion }
          : {}),
        ...(current.costStatus === "unknown" || measurements.costStatus === "unknown"
          ? { costStatus: "unknown" as const }
          : measurements.costStatus ?? current.costStatus
            ? { costStatus: measurements.costStatus ?? current.costStatus }
            : {}),
        coverage: this.sawUsageValue
          ? this.sawUsageGap
            ? "partial"
            : "complete"
          : "unavailable",
        missingReasons: [
          ...(current.coverage === "unavailable" ? [] : current.missingReasons ?? []),
          ...(measurements.missingReasons ?? []),
        ],
      };
    }

    if (observation.kind === "run.end") {
      this.value.finishedAt = observation.timestamp;
      this.value.durations.wallMs = observation.measurements?.durationMs;
      this.value.durations.queueMs = observation.measurements?.queueDurationMs ?? this.value.durations.queueMs;
      const outcome = asString(attrs["openneko.outcome"]);
      this.value.status =
        outcome === "cancelled"
          ? "cancelled"
          : observation.status === "error"
            ? "failed"
            : "completed";
      this.recordError(observation);
      this.value.telemetryComplete = this.value.usage.coverage === "complete";
    }
  }

  private recordError(observation: HarnessObservation): void {
    this.value.errorType ??= observation.errorType;
    this.value.errorCode ??= observation.errorCode;
    this.value.errorMessage ??= observation.errorMessage;
  }

  snapshot(): HarnessRunSummary {
    return structuredClone(this.value);
  }
}
