import { describe, expect, it } from "vitest";
import {
  createWorkerHarnessObserver,
  isWorkerTelemetryEnabled,
} from "../src/telemetry";

describe("worker telemetry", () => {
  it("stays disabled unless explicitly enabled or an OTLP endpoint is set", () => {
    expect(isWorkerTelemetryEnabled({})).toBe(false);
    expect(
      isWorkerTelemetryEnabled({
        OTEL_EXPORTER_OTLP_TRACES_ENDPOINT: "http://collector:4318/v1/traces",
      }),
    ).toBe(true);
    expect(
      isWorkerTelemetryEnabled({
        OPENNEKO_OTEL_ENABLED: "true",
      }),
    ).toBe(true);
    expect(
      isWorkerTelemetryEnabled({
        OPENNEKO_OTEL_ENABLED: "true",
        OTEL_SDK_DISABLED: "true",
      }),
    ).toBe(false);
  });

  it("always produces a local content-free run summary", async () => {
    const telemetry = createWorkerHarnessObserver("job-3");
    await telemetry.observer.observe({
      kind: "run.start",
      operationId: "run-3",
      attributes: {
        "openneko.backend": "ax",
        prompt: "private",
      },
    });
    await telemetry.observer.observe({
      kind: "run.end",
      operationId: "run-3",
      status: "ok",
      measurements: { durationMs: 12, coverage: "unavailable" },
    });
    expect(telemetry.snapshot()).toMatchObject({
      runId: "job-3",
      backend: "ax",
      status: "completed",
      durations: { wallMs: 12 },
    });
    expect(JSON.stringify(telemetry.snapshot())).not.toContain("private");
  });
});
