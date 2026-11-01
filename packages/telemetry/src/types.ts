export const OBSERVATION_SCHEMA_VERSION = "openneko.observation/v1" as const;

export const OBSERVATION_KINDS = [
  "run.start",
  "run.first_output",
  "run.end",
  "stage.start",
  "stage.end",
  "model.request",
  "model.first_chunk",
  "model.response",
  "tool.start",
  "tool.end",
  "delegation.start",
  "delegation.end",
  "memory.start",
  "memory.end",
  "policy.decision",
  "approval.decision",
  "validation.result",
  "retry",
  "output.contract",
  "error",
] as const;

export type ObservationKind = (typeof OBSERVATION_KINDS)[number];
export type ObservationStatus = "unset" | "ok" | "error";
export type AttributeValue = string | number | boolean | readonly string[];

export type NormalizedUsage = {
  inputTokens?: number;
  outputTokens?: number;
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
  reasoningTokens?: number;
  totalTokens?: number;
  estimatedCostUsd?: number;
  billedCostUsd?: number;
  currency?: string;
  pricingCatalogVersion?: string;
  costStatus?: "actual" | "estimated" | "included" | "unknown";
  costSource?: string;
  coverage: "complete" | "partial" | "unavailable";
  missingReasons?: readonly string[];
};

export type ObservationMeasurements = NormalizedUsage & {
  durationMs?: number;
  queueDurationMs?: number;
  firstOutputMs?: number;
  inputBytes?: number;
  outputBytes?: number;
  compactedBytes?: number;
  rows?: number;
  attempts?: number;
  acceptedRows?: number;
  processedRows?: number;
  finalRows?: number;
  chunkCount?: number;
  artifactBytes?: number;
};

/**
 * Provider-neutral internal telemetry. It deliberately carries metadata and
 * measurements, never a prompt, response, query, tool input, or tool result.
 */
export type HarnessObservation = {
  schemaVersion: typeof OBSERVATION_SCHEMA_VERSION;
  sequence: number;
  kind: ObservationKind;
  timestamp: string;
  runId: string;
  operationId: string;
  parentOperationId?: string;
  traceId?: string;
  spanId?: string;
  status: ObservationStatus;
  attributes: Readonly<Record<string, AttributeValue>>;
  measurements?: Readonly<ObservationMeasurements>;
  errorType?: string;
  errorCode?: string;
  errorMessage?: string;
};

export type ObservationInput = Omit<
  HarnessObservation,
  "schemaVersion" | "sequence" | "timestamp" | "runId" | "attributes" | "status"
> & {
  timestamp?: string;
  status?: ObservationStatus;
  attributes?: Record<string, unknown>;
};

export interface ObservationSink {
  emit(observation: HarnessObservation): Promise<void> | void;
  flush?(): Promise<void> | void;
  shutdown?(): Promise<void> | void;
}

export type HarnessObserver = {
  observe(input: ObservationInput): Promise<HarnessObservation>;
  flush(): Promise<void>;
  shutdown(): Promise<void>;
};
