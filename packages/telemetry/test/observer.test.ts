import { describe, expect, it } from "vitest";
import {
  HarnessRunSummaryAccumulator,
  MemoryObservationSink,
  createHarnessObserver,
  observeSafely,
} from "../src";

describe("harness observations", () => {
  it("orders events and removes content-bearing or secret attributes", async () => {
    const memory = new MemoryObservationSink();
    const observer = createHarnessObserver({
      runId: "run-1",
      sinks: [memory],
      now: () => new Date("2026-08-10T00:00:00.000Z"),
    });

    await observer.observe({
      kind: "model.request",
      operationId: "model-1",
      attributes: {
        "gen_ai.provider.name": "anthropic",
        "gen_ai.response.model": "claude-sonnet",
        "gen_ai.response.text": "must also be removed",
        prompt: "do not retain me",
        harmless: "Bearer top-secret-credential",
      },
    });
    await observer.observe({ kind: "model.response", operationId: "model-1" });

    expect(memory.observations.map((item) => item.sequence)).toEqual([1, 2]);
    expect(memory.observations[0]?.attributes).toEqual({
      "gen_ai.provider.name": "anthropic",
      "gen_ai.response.model": "claude-sonnet",
      harmless: "[REDACTED]",
    });
  });

  it("never lets a broken sink change the harness outcome", async () => {
    const memory = new MemoryObservationSink();
    const observer = createHarnessObserver({
      runId: "run-1",
      sinks: [
        { emit: () => Promise.reject(new Error("collector unavailable")) },
        memory,
      ],
    });

    await expect(
      observer.observe({ kind: "run.start", operationId: "run-1" }),
    ).resolves.toBeDefined();
    expect(memory.observations).toHaveLength(1);
  });

  it("contains failures from an arbitrary injected observer", async () => {
    await expect(
      observeSafely(
        {
          observe: async () => {
            throw new Error("broken instrumentation");
          },
          flush: async () => {},
          shutdown: async () => {},
        },
        { kind: "run.start", operationId: "run-1" },
      ),
    ).resolves.toBeUndefined();
  });

  it("accumulates a production-safe run summary", async () => {
    const summary = new HarnessRunSummaryAccumulator("run-1");
    const observer = createHarnessObserver({ runId: "run-1", sinks: [summary] });
    await observer.observe({
      kind: "run.start",
      operationId: "run-1",
      attributes: { "openneko.backend": "ax" },
    });
    await observer.observe({
      kind: "model.request",
      operationId: "model-1",
      measurements: { inputBytes: 120, coverage: "unavailable" },
    });
    await observer.observe({
      kind: "model.response",
      operationId: "model-1",
      measurements: {
        inputTokens: 10,
        outputTokens: 5,
        totalTokens: 15,
        outputBytes: 48,
        coverage: "complete",
      },
    });
    await observer.observe({
      kind: "run.end",
      operationId: "run-1",
      status: "ok",
      measurements: { durationMs: 42, coverage: "complete" },
    });

    expect(summary.snapshot()).toMatchObject({
      status: "completed",
      backend: "ax",
      counts: { inference: 1 },
      usage: { inputTokens: 10, outputTokens: 5, totalTokens: 15 },
      io: { inputBytes: 120, outputBytes: 48 },
      durations: { wallMs: 42 },
      telemetryComplete: true,
    });
  });

  it("reports partial usage when some model usage is present and some is missing", async () => {
    const summary = new HarnessRunSummaryAccumulator("run-2");
    summary.emit({
      schemaVersion: "openneko.observation/v1",
      sequence: 1,
      kind: "model.response",
      timestamp: "2026-08-10T00:00:00.000Z",
      runId: "run-2",
      operationId: "model-1",
      status: "ok",
      attributes: {},
      measurements: { totalTokens: 15, coverage: "complete" },
    });
    summary.emit({
      schemaVersion: "openneko.observation/v1",
      sequence: 2,
      kind: "model.response",
      timestamp: "2026-08-10T00:00:01.000Z",
      runId: "run-2",
      operationId: "model-2",
      status: "ok",
      attributes: {},
      measurements: {
        coverage: "unavailable",
        missingReasons: ["provider omitted usage"],
      },
    });
    expect(summary.snapshot().usage).toMatchObject({
      totalTokens: 15,
      coverage: "partial",
      missingReasons: ["provider omitted usage"],
    });
  });

  it("marks a completed run failed when a downstream contract gate fails", async () => {
    const summary = new HarnessRunSummaryAccumulator("run-3");
    const observer = createHarnessObserver({ runId: "run-3", sinks: [summary] });
    await observer.observe({ kind: "run.start", operationId: "run-3" });
    await observer.observe({
      kind: "run.end",
      operationId: "run-3",
      status: "ok",
      attributes: { "openneko.outcome": "completed" },
    });
    await observer.observe({
      kind: "validation.result",
      operationId: "run-3:persistence-contract",
      parentOperationId: "run-3",
      status: "error",
      errorType: "invalid_output",
    });
    expect(summary.snapshot()).toMatchObject({
      status: "failed",
      errorType: "invalid_output",
      counts: { validations: 1 },
    });
  });

  it("keeps a redacted, bounded error code and message on a failed run", async () => {
    const summary = new HarnessRunSummaryAccumulator("run-4");
    const memory = new MemoryObservationSink();
    const observer = createHarnessObserver({ runId: "run-4", sinks: [summary, memory] });
    await observer.observe({ kind: "run.start", operationId: "run-4" });
    await observer.observe({
      kind: "model.request",
      operationId: "model-1",
      errorCode: "ignored",
      errorMessage: "ignored",
    });
    await observer.observe({
      kind: "run.end",
      operationId: "run-4",
      status: "error",
      errorType: "agent_backend_error",
      errorCode: "model http 429",
      errorMessage: `provider rejected key sk-abcdefghijklmnop ${"x".repeat(600)}`,
    });
    expect(memory.observations[1]).not.toHaveProperty("errorCode");
    const end = memory.observations[2];
    expect(end?.errorCode).toBe("model_http_429");
    expect(end?.errorMessage).toMatch(/^provider rejected key \[REDACTED\] x+$/);
    expect(end?.errorMessage).toHaveLength(512);
    expect(summary.snapshot()).toMatchObject({
      status: "failed",
      errorType: "agent_backend_error",
      errorCode: "model_http_429",
      errorMessage: end?.errorMessage,
    });
  });
});
