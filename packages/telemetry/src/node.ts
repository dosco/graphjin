import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-proto";
import { resourceFromAttributes } from "@opentelemetry/resources";
import { BatchSpanProcessor } from "@opentelemetry/sdk-trace-base";
import { NodeTracerProvider } from "@opentelemetry/sdk-trace-node";
import {
  ATTR_DEPLOYMENT_ENVIRONMENT_NAME,
  ATTR_SERVICE_NAME,
  ATTR_SERVICE_VERSION,
} from "@opentelemetry/semantic-conventions";
import { OpenTelemetryObservationSink } from "./opentelemetry";

export type OtlpTelemetryRuntimeOptions = {
  serviceName: string;
  serviceVersion?: string;
  deploymentEnvironment?: string;
  /** OTLP/HTTP protobuf traces endpoint, normally ending in /v1/traces. */
  endpoint?: string;
  /** Static exporter headers. Never add these values to observations/logs. */
  headers?: Record<string, string>;
};

export type OtlpTelemetryRuntime = {
  sink: OpenTelemetryObservationSink;
  forceFlush(): Promise<void>;
  shutdown(): Promise<void>;
};

/** Create and globally register a Node OTLP trace provider. */
export function createOtlpTelemetryRuntime(
  options: OtlpTelemetryRuntimeOptions,
): OtlpTelemetryRuntime {
  const exporter = new OTLPTraceExporter({
    ...(options.endpoint ? { url: options.endpoint } : {}),
    ...(options.headers ? { headers: options.headers } : {}),
  });
  const processor = new BatchSpanProcessor(exporter);
  const resource = resourceFromAttributes({
    [ATTR_SERVICE_NAME]: options.serviceName,
    ...(options.serviceVersion
      ? { [ATTR_SERVICE_VERSION]: options.serviceVersion }
      : {}),
    ...(options.deploymentEnvironment
      ? {
          [ATTR_DEPLOYMENT_ENVIRONMENT_NAME]: options.deploymentEnvironment,
        }
      : {}),
  });
  const provider = new NodeTracerProvider({
    resource,
    spanProcessors: [processor],
  });
  provider.register();
  let stopped = false;
  const forceFlush = async () => {
    if (!stopped) await provider.forceFlush();
  };
  const shutdown = async () => {
    if (stopped) return;
    stopped = true;
    await provider.shutdown();
  };
  return {
    sink: new OpenTelemetryObservationSink({
      tracer: provider.getTracer("io.openneko.harness", "0.1.0"),
      forceFlush,
      shutdown,
    }),
    forceFlush,
    shutdown,
  };
}
