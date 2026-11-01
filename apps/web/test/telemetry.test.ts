import { describe, expect, it } from "vitest";
import {
  createWebHarnessObserver,
  isWebTelemetryEnabled,
} from "../src/lib/telemetry";

describe("web telemetry", () => {
  it("stays disabled unless explicitly enabled or an OTLP endpoint is set", () => {
    expect(isWebTelemetryEnabled({})).toBe(false);
    expect(
      isWebTelemetryEnabled({
        OTEL_EXPORTER_OTLP_TRACES_ENDPOINT: "http://collector:4318/v1/traces",
      }),
    ).toBe(true);
    expect(
      isWebTelemetryEnabled({
        OPENNEKO_OTEL_ENABLED: "true",
        OTEL_SDK_DISABLED: "true",
      }),
    ).toBe(false);
  });

  it("always produces a local content-free run summary", async () => {
    const telemetry = createWebHarnessObserver("web-run-1");
    await telemetry.observer.observe({
      kind: "run.start",
      operationId: "work:web-run-1",
      attributes: {
        "openneko.product.path": "work",
        prompt: "private customer question",
      },
    });
    await telemetry.observer.observe({
      kind: "run.end",
      operationId: "work:web-run-1",
      status: "ok",
      measurements: { durationMs: 25, coverage: "unavailable" },
    });
    expect(telemetry.snapshot()).toMatchObject({
      runId: "web-run-1",
      productPath: "work",
      status: "completed",
      durations: { wallMs: 25 },
    });
    expect(JSON.stringify(telemetry.snapshot())).not.toContain("customer question");
  });
});
