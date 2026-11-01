import { context, propagation, trace } from "@opentelemetry/api";
import { InMemorySpanExporter, SimpleSpanProcessor } from "@opentelemetry/sdk-trace-base";
import { NodeTracerProvider } from "@opentelemetry/sdk-trace-node";
import { afterAll, describe, expect, it } from "vitest";
import {
  OpenTelemetryObservationSink,
  createHarnessObserver,
  withRunTraceContext,
} from "../src";

// register() installs the W3C propagator and the async context manager, as
// the worker's OTLP runtime does.
const provider = new NodeTracerProvider({
  spanProcessors: [new SimpleSpanProcessor(new InMemorySpanExporter())],
});
provider.register();
afterAll(() => provider.shutdown());

function injected(): Record<string, string> {
  const carrier: Record<string, string> = {};
  propagation.inject(context.active(), carrier);
  return carrier;
}

describe("withRunTraceContext", () => {
  it("makes the run's root span the parent of outgoing requests", async () => {
    const sink = new OpenTelemetryObservationSink({ tracer: provider.getTracer("test") });
    const observer = createHarnessObserver({ runId: "run-a", sinks: [sink] });
    await observer.observe({ kind: "run.start", operationId: "op-a", timestamp: new Date().toISOString() });

    const headers = await withRunTraceContext("run-a", async () => {
      await Promise.resolve();
      return injected();
    });
    const traceId = headers.traceparent?.split("-")[1];
    expect(traceId).toMatch(/^[0-9a-f]{32}$/);
    expect(trace.getActiveSpan()).toBeUndefined();

    await observer.observe({ kind: "run.end", operationId: "op-a", timestamp: new Date().toISOString() });
    expect(withRunTraceContext("run-a", injected)).toEqual({});
  });

  it("runs unchanged for an unknown run", () => {
    expect(withRunTraceContext("missing", () => 42)).toBe(42);
    expect(withRunTraceContext("missing", injected)).toEqual({});
  });
});
