import {
  sanitizeAttributes,
  sanitizeErrorMessage,
  sanitizeErrorType,
} from "./redaction";
import {
  OBSERVATION_SCHEMA_VERSION,
  type HarnessObservation,
  type HarnessObserver,
  type ObservationInput,
  type ObservationSink,
} from "./types";

export const NOOP_OBSERVATION_SINK: ObservationSink = {
  emit: () => {},
};

export class MemoryObservationSink implements ObservationSink {
  readonly observations: HarnessObservation[] = [];

  emit(observation: HarnessObservation): void {
    this.observations.push(observation);
  }
}

/** A string `code` on an error (Node system errors, OpenNeko typed failures). */
export function errorCodeOf(error: unknown): string | undefined {
  const code = (error as { code?: unknown } | null | undefined)?.code;
  return typeof code === "string" ? code : undefined;
}

/** Invoke any observer without allowing instrumentation failure to escape. */
export async function observeSafely(
  observer: HarnessObserver | undefined,
  input: ObservationInput,
): Promise<void> {
  try {
    await observer?.observe(input);
  } catch {
    // Telemetry must never change a product-path outcome.
  }
}

const ERROR_DETAIL_KINDS = new Set<ObservationInput["kind"]>([
  "run.end",
  "stage.end",
  "error",
]);

export function createHarnessObserver(options: {
  runId: string;
  sinks?: readonly ObservationSink[];
  now?: () => Date;
  onSinkError?: (cause: unknown, observation: HarnessObservation) => void;
}): HarnessObserver {
  const sinks = options.sinks?.length ? options.sinks : [NOOP_OBSERVATION_SINK];
  const now = options.now ?? (() => new Date());
  let sequence = 0;

  async function callWithoutAffectingRun(
    sink: ObservationSink,
    method: "emit" | "flush" | "shutdown",
    observation?: HarnessObservation,
  ): Promise<void> {
    try {
      if (method === "emit" && observation) await sink.emit(observation);
      else if (method === "flush") await sink.flush?.();
      else if (method === "shutdown") await sink.shutdown?.();
    } catch (cause) {
      if (observation) options.onSinkError?.(cause, observation);
    }
  }

  return {
    async observe(input: ObservationInput): Promise<HarnessObservation> {
      const detailed = ERROR_DETAIL_KINDS.has(input.kind);
      const errorCode = detailed ? sanitizeErrorType(input.errorCode) : undefined;
      const errorMessage = detailed
        ? sanitizeErrorMessage(input.errorMessage)
        : undefined;
      const observation: HarnessObservation = {
        schemaVersion: OBSERVATION_SCHEMA_VERSION,
        sequence: ++sequence,
        kind: input.kind,
        timestamp: input.timestamp ?? now().toISOString(),
        runId: options.runId,
        operationId: input.operationId,
        ...(input.parentOperationId
          ? { parentOperationId: input.parentOperationId }
          : {}),
        ...(input.traceId ? { traceId: input.traceId } : {}),
        ...(input.spanId ? { spanId: input.spanId } : {}),
        status: input.status ?? "unset",
        attributes: sanitizeAttributes(input.attributes),
        ...(input.measurements ? { measurements: { ...input.measurements } } : {}),
        ...(sanitizeErrorType(input.errorType)
          ? { errorType: sanitizeErrorType(input.errorType) }
          : {}),
        ...(errorCode ? { errorCode } : {}),
        ...(errorMessage ? { errorMessage } : {}),
      };
      await Promise.all(
        sinks.map((sink) => callWithoutAffectingRun(sink, "emit", observation)),
      );
      return observation;
    },
    async flush(): Promise<void> {
      await Promise.all(sinks.map((sink) => callWithoutAffectingRun(sink, "flush")));
    },
    async shutdown(): Promise<void> {
      await Promise.all(
        sinks.map((sink) => callWithoutAffectingRun(sink, "shutdown")),
      );
    },
  };
}
