import { SpanKind, SpanStatusCode } from "@opentelemetry/api";
import {
  BasicTracerProvider,
  InMemorySpanExporter,
  SimpleSpanProcessor,
} from "@opentelemetry/sdk-trace-base";
import { describe, expect, it } from "vitest";
import {
  OpenTelemetryObservationSink,
  createHarnessObserver,
  type HarnessObservation,
} from "../src";

function setup() {
  const exporter = new InMemorySpanExporter();
  const provider = new BasicTracerProvider({
    spanProcessors: [new SimpleSpanProcessor(exporter)],
  });
  const sink = new OpenTelemetryObservationSink({
    tracer: provider.getTracer("openneko-test"),
    forceFlush: () => provider.forceFlush(),
    shutdown: () => provider.shutdown(),
  });
  return { exporter, provider, sink };
}

describe("OpenTelemetryObservationSink", () => {
  it("creates correlated agent/model spans with GenAI usage attributes", async () => {
    const { exporter, sink } = setup();
    const observer = createHarnessObserver({ runId: "job-1", sinks: [sink] });
    await observer.observe({
      kind: "run.start",
      operationId: "run-1",
      timestamp: "2026-08-10T00:00:00.000Z",
      attributes: {
        "openneko.backend": "ax",
        prompt: "must not be exported",
      },
    });
    await observer.observe({
      kind: "model.request",
      operationId: "model-1",
      parentOperationId: "run-1",
      timestamp: "2026-08-10T00:00:00.100Z",
      attributes: {
        "gen_ai.provider.name": "anthropic",
        "gen_ai.request.model": "claude-sonnet-4-5",
      },
    });
    await observer.observe({
      kind: "model.response",
      operationId: "model-1",
      parentOperationId: "run-1",
      timestamp: "2026-08-10T00:00:01.000Z",
      status: "ok",
      attributes: { "gen_ai.response.model": "claude-sonnet-4-5" },
      measurements: {
        inputTokens: 20,
        outputTokens: 8,
        cacheReadTokens: 4,
        reasoningTokens: 2,
        totalTokens: 28,
        billedCostUsd: 0.004,
        coverage: "complete",
      },
    });
    await observer.observe({
      kind: "run.first_output",
      operationId: "run-1",
      timestamp: "2026-08-10T00:00:00.500Z",
      measurements: { firstOutputMs: 500, coverage: "unavailable" },
    });
    await observer.observe({
      kind: "run.end",
      operationId: "run-1",
      timestamp: "2026-08-10T00:00:01.100Z",
      status: "ok",
      measurements: { durationMs: 1_100, coverage: "complete" },
    });
    await observer.flush();

    const spans = exporter.getFinishedSpans();
    const root = spans.find((span) => span.name === "invoke_agent openneko");
    const model = spans.find(
      (span) => span.name === "chat claude-sonnet-4-5",
    );
    expect(root).toBeDefined();
    expect(model).toBeDefined();
    expect(model?.kind).toBe(SpanKind.CLIENT);
    expect(model?.parentSpanContext?.spanId).toBe(root?.spanContext().spanId);
    expect(model?.attributes).toMatchObject({
      "gen_ai.operation.name": "chat",
      "gen_ai.provider.name": "anthropic",
      "gen_ai.usage.input_tokens": 20,
      "gen_ai.usage.output_tokens": 8,
      "gen_ai.usage.cache_read.input_tokens": 4,
      "gen_ai.usage.reasoning.output_tokens": 2,
      "openneko.usage.total_tokens": 28,
      "openneko.usage.billed_cost_usd": 0.004,
      "openneko.telemetry.coverage": "complete",
    });
    expect(root?.events.map((event) => event.name)).toContain(
      "run.first_output",
    );
    expect(root?.attributes).not.toHaveProperty("prompt");
    await sink.shutdown();
  });

  it("defensively removes content and closes incomplete spans on shutdown", async () => {
    const exporter = new InMemorySpanExporter();
    const provider = new BasicTracerProvider({
      spanProcessors: [new SimpleSpanProcessor(exporter)],
    });
    const sink = new OpenTelemetryObservationSink({
      tracer: provider.getTracer("openneko-test"),
      forceFlush: () => provider.forceFlush(),
    });
    const observation: HarnessObservation = {
      schemaVersion: "openneko.observation/v1",
      sequence: 1,
      kind: "tool.start",
      timestamp: "2026-08-10T00:00:00.000Z",
      runId: "job-2",
      operationId: "tool-1",
      status: "unset",
      attributes: {
        "gen_ai.tool.name": "execute_graphql",
        "tool.input": "select private_data",
      },
    };
    sink.emit(observation);
    await sink.shutdown();

    const span = exporter.getFinishedSpans()[0];
    expect(span?.name).toBe("execute_tool execute_graphql");
    expect(span?.attributes).not.toHaveProperty("tool.input");
    expect(span?.attributes["openneko.telemetry.incomplete"]).toBe(true);
    expect(span?.status.code).toBe(SpanStatusCode.ERROR);
    await provider.shutdown();
  });

  it("exports the error message as the span status message", async () => {
    const { exporter, sink } = setup();
    const observer = createHarnessObserver({ runId: "job-3", sinks: [sink] });
    await observer.observe({ kind: "run.start", operationId: "run-3" });
    await observer.observe({
      kind: "run.end",
      operationId: "run-3",
      status: "error",
      errorType: "agent_backend_error",
      errorCode: "deadline_exceeded",
      errorMessage: "run deadline exceeded after 300s",
    });
    const span = exporter.getFinishedSpans()[0];
    expect(span?.status).toEqual({
      code: SpanStatusCode.ERROR,
      message: "run deadline exceeded after 300s",
    });
    expect(span?.attributes["openneko.error.code"]).toBe("deadline_exceeded");
    expect(span?.attributes["error.type"]).toBe("agent_backend_error");
    await sink.shutdown();
  });
});
