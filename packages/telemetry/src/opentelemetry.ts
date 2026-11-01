import {
  SpanKind,
  SpanStatusCode,
  context,
  trace,
  type Attributes,
  type Span,
  type Tracer,
} from "@opentelemetry/api";
import {
  sanitizeAttributes,
  sanitizeErrorMessage,
  sanitizeErrorType,
} from "./redaction";
import type {
  HarnessObservation,
  ObservationMeasurements,
  ObservationSink,
} from "./types";

const START_KINDS = new Set<HarnessObservation["kind"]>([
  "run.start",
  "stage.start",
  "model.request",
  "tool.start",
  "delegation.start",
  "memory.start",
]);

const END_KINDS = new Set<HarnessObservation["kind"]>([
  "run.end",
  "stage.end",
  "model.response",
  "tool.end",
  "delegation.end",
  "memory.end",
]);

export type OpenTelemetryObservationSinkOptions = {
  tracer: Tracer;
  /** Used by the Node runtime to flush its provider/exporter. */
  forceFlush?: () => Promise<void>;
  /** Used by the Node runtime to shut down its provider/exporter. */
  shutdown?: () => Promise<void>;
};

// Root span of each run in progress, across sinks, so code that runs on a
// run's behalf (for example a GraphJin call) can join the run's trace.
const runSpans = new Map<string, Span>();

/**
 * Run fn with the run's root span as the active OpenTelemetry context, so
 * outgoing requests carry the run's trace context. Without telemetry, or for
 * a run that has no root span, fn runs unchanged.
 */
export function withRunTraceContext<T>(runId: string, fn: () => T): T {
  const span = runSpans.get(runId);
  if (!span) return fn();
  return context.with(trace.setSpan(context.active(), span), fn);
}

type ActiveSpan = {
  span: Span;
  runId: string;
  operationId: string;
};

/**
 * Converts the content-free OpenNeko observation contract into OTel traces.
 * The sink defensively sanitizes attributes again in case it is used without
 * createHarnessObserver(). It never exports prompt/query/tool/result content.
 */
export class OpenTelemetryObservationSink implements ObservationSink {
  private readonly active = new Map<string, ActiveSpan>();
  private readonly roots = new Map<string, Span>();

  constructor(private readonly options: OpenTelemetryObservationSinkOptions) {}

  emit(observation: HarnessObservation): void {
    if (START_KINDS.has(observation.kind)) {
      this.start(observation);
      return;
    }
    if (END_KINDS.has(observation.kind)) {
      this.end(observation);
      return;
    }
    this.addEvent(observation);
  }

  async flush(): Promise<void> {
    await this.options.forceFlush?.();
  }

  async shutdown(): Promise<void> {
    for (const active of this.active.values()) {
      active.span.setAttribute("openneko.telemetry.incomplete", true);
      active.span.setStatus({
        code: SpanStatusCode.ERROR,
        message: "observation stream ended before the operation completed",
      });
      active.span.end();
    }
    this.active.clear();
    for (const [runId, span] of this.roots) {
      if (runSpans.get(runId) === span) runSpans.delete(runId);
    }
    this.roots.clear();
    await this.options.forceFlush?.();
    await this.options.shutdown?.();
  }

  private start(observation: HarnessObservation): void {
    const existing = this.active.get(observation.operationId);
    if (existing) {
      existing.span.addEvent("openneko.duplicate_start", eventAttributes(observation));
      return;
    }

    const parent = observation.parentOperationId
      ? this.active.get(observation.parentOperationId)?.span
      : undefined;
    const parentContext = parent
      ? trace.setSpan(context.active(), parent)
      : context.active();
    const span = this.options.tracer.startSpan(
      spanName(observation),
      {
        kind: spanKind(observation.kind),
        startTime: new Date(observation.timestamp),
        attributes: spanAttributes(observation),
      },
      parentContext,
    );
    applyStatus(span, observation);
    this.active.set(observation.operationId, {
      span,
      runId: observation.runId,
      operationId: observation.operationId,
    });
    if (observation.kind === "run.start") {
      this.roots.set(observation.runId, span);
      runSpans.set(observation.runId, span);
    }
  }

  private end(observation: HarnessObservation): void {
    const active = this.active.get(observation.operationId);
    if (!active) {
      // Preserve telemetry from a partial/restarted stream as a zero-duration
      // span rather than dropping it. Durable eval state remains authoritative.
      this.start({
        ...observation,
        kind: matchingStartKind(observation.kind),
      });
    }
    const resolved = this.active.get(observation.operationId);
    if (!resolved) return;
    resolved.span.setAttributes(spanAttributes(observation));
    applyStatus(resolved.span, observation);
    resolved.span.end(new Date(observation.timestamp));
    this.active.delete(observation.operationId);
    if (observation.kind === "run.end") {
      this.roots.delete(observation.runId);
      if (runSpans.get(observation.runId) === resolved.span) runSpans.delete(observation.runId);
    }
  }

  private addEvent(observation: HarnessObservation): void {
    const span =
      this.active.get(observation.operationId)?.span ??
      (observation.parentOperationId
        ? this.active.get(observation.parentOperationId)?.span
        : undefined) ??
      this.roots.get(observation.runId);
    if (!span) {
      const fallback = this.options.tracer.startSpan(
        `openneko.event ${observation.kind}`,
        {
          kind: SpanKind.INTERNAL,
          startTime: new Date(observation.timestamp),
          attributes: spanAttributes(observation),
        },
      );
      applyStatus(fallback, observation);
      fallback.end(new Date(observation.timestamp));
      return;
    }
    span.addEvent(
      observation.kind,
      eventAttributes(observation),
      new Date(observation.timestamp),
    );
    if (observation.status === "error" || observation.kind === "error") {
      applyStatus(span, { ...observation, status: "error" });
    }
  }
}

function matchingStartKind(
  kind: HarnessObservation["kind"],
): HarnessObservation["kind"] {
  switch (kind) {
    case "run.end":
      return "run.start";
    case "stage.end":
      return "stage.start";
    case "model.response":
      return "model.request";
    case "tool.end":
      return "tool.start";
    case "delegation.end":
      return "delegation.start";
    case "memory.end":
      return "memory.start";
    default:
      return kind;
  }
}

function spanName(observation: HarnessObservation): string {
  const attrs = observation.attributes;
  switch (observation.kind) {
    case "run.start":
      return "invoke_agent openneko";
    case "stage.start":
      return `openneko.stage ${stringAttribute(attrs["openneko.stage"]) ?? "unknown"}`;
    case "model.request":
      return `chat ${stringAttribute(attrs["gen_ai.request.model"]) ?? "unknown"}`;
    case "tool.start":
      return `execute_tool ${stringAttribute(attrs["gen_ai.tool.name"]) ?? "unknown"}`;
    case "delegation.start":
      return `invoke_agent ${stringAttribute(attrs["openneko.delegation.target"]) ?? "unknown"}`;
    case "memory.start":
      return "openneko.memory";
    default:
      return `openneko ${observation.kind}`;
  }
}

function spanKind(kind: HarnessObservation["kind"]): SpanKind {
  return kind === "model.request" ? SpanKind.CLIENT : SpanKind.INTERNAL;
}

function stringAttribute(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function spanAttributes(observation: HarnessObservation): Attributes {
  const attributes: Attributes = {
    ...otelAttributes(sanitizeAttributes(observation.attributes)),
    ...measurementAttributes(observation.measurements),
    "openneko.observation.schema_version": observation.schemaVersion,
    "openneko.observation.sequence": observation.sequence,
    "openneko.run.id": observation.runId,
  };
  const operationName = genAiOperationName(observation.kind);
  if (operationName) attributes["gen_ai.operation.name"] = operationName;
  const errorType = sanitizeErrorType(observation.errorType);
  if (errorType) attributes["error.type"] = errorType;
  const errorCode = sanitizeErrorType(observation.errorCode);
  if (errorCode) attributes["openneko.error.code"] = errorCode;
  return attributes;
}

function eventAttributes(observation: HarnessObservation): Attributes {
  return {
    ...spanAttributes(observation),
    "openneko.observation.kind": observation.kind,
  };
}

function genAiOperationName(
  kind: HarnessObservation["kind"],
): string | undefined {
  if (kind === "run.start" || kind === "delegation.start") return "invoke_agent";
  if (kind === "model.request") return "chat";
  if (kind === "tool.start") return "execute_tool";
  return undefined;
}

function otelAttributes(
  values: Readonly<Record<string, string | number | boolean | readonly string[]>>,
): Attributes {
  return Object.fromEntries(
    Object.entries(values).map(([key, value]) => [
      key,
      Array.isArray(value) ? [...value] : value,
    ]),
  ) as Attributes;
}

function measurementAttributes(
  measurements: Readonly<ObservationMeasurements> | undefined,
): Attributes {
  if (!measurements) return {};
  const attrs: Attributes = {
    "openneko.telemetry.coverage": measurements.coverage,
  };
  assignNumber(attrs, "gen_ai.usage.input_tokens", measurements.inputTokens);
  assignNumber(attrs, "gen_ai.usage.output_tokens", measurements.outputTokens);
  assignNumber(
    attrs,
    "gen_ai.usage.cache_read.input_tokens",
    measurements.cacheReadTokens,
  );
  assignNumber(
    attrs,
    "gen_ai.usage.cache_creation.input_tokens",
    measurements.cacheWriteTokens,
  );
  assignNumber(
    attrs,
    "gen_ai.usage.reasoning.output_tokens",
    measurements.reasoningTokens,
  );
  assignNumber(attrs, "openneko.usage.total_tokens", measurements.totalTokens);
  assignNumber(
    attrs,
    "openneko.usage.estimated_cost_usd",
    measurements.estimatedCostUsd,
  );
  assignNumber(
    attrs,
    "openneko.usage.billed_cost_usd",
    measurements.billedCostUsd,
  );
  assignNumber(attrs, "openneko.duration.ms", measurements.durationMs);
  assignNumber(attrs, "openneko.queue.duration.ms", measurements.queueDurationMs);
  assignNumber(
    attrs,
    "openneko.first_output.duration.ms",
    measurements.firstOutputMs,
  );
  assignNumber(attrs, "openneko.input.bytes", measurements.inputBytes);
  assignNumber(attrs, "openneko.output.bytes", measurements.outputBytes);
  assignNumber(attrs, "openneko.compacted.bytes", measurements.compactedBytes);
  assignNumber(attrs, "openneko.rows", measurements.rows);
  assignNumber(attrs, "openneko.attempts", measurements.attempts);
  if (measurements.currency) attrs["openneko.usage.currency"] = measurements.currency;
  if (measurements.costStatus) attrs["openneko.usage.cost_status"] = measurements.costStatus;
  if (measurements.costSource) attrs["openneko.usage.cost_source"] = measurements.costSource;
  if (measurements.pricingCatalogVersion) {
    attrs["openneko.usage.pricing_catalog.version"] =
      measurements.pricingCatalogVersion;
  }
  if (measurements.missingReasons?.length) {
    // Reasons may be useful internally but are not exported as free-form text.
    attrs["openneko.telemetry.missing_reason_count"] =
      measurements.missingReasons.length;
  }
  return attrs;
}

function assignNumber(
  attributes: Attributes,
  key: string,
  value: number | undefined,
): void {
  if (value !== undefined && Number.isFinite(value)) attributes[key] = value;
}

function applyStatus(span: Span, observation: HarnessObservation): void {
  if (observation.status === "error") {
    const message =
      sanitizeErrorMessage(observation.errorMessage) ??
      sanitizeErrorType(observation.errorType);
    span.setStatus({
      code: SpanStatusCode.ERROR,
      ...(message ? { message } : {}),
    });
  } else if (observation.status === "ok") {
    span.setStatus({ code: SpanStatusCode.OK });
  }
}
