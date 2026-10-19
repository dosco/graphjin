import { describe, expect, it } from "vitest";
import {
  MemoryObservationSink,
  createHarnessObserver,
} from "@neko/telemetry";
import { createAgentEventTelemetry } from "../src/work/agent-event-telemetry";

describe("agent event telemetry", () => {
  it("exports metadata and byte counts without prompt, payload, or tool content", async () => {
    const sink = new MemoryObservationSink();
    const observer = createHarnessObserver({ runId: "run-1", sinks: [sink] });
    const telemetry = createAgentEventTelemetry({
      observer,
      operationId: "workflow:run-1",
    });
    const secret = "onk_wf_deadbeefdead_do-not-export-this-token";
    const payload = "customer-private-payload-value";

    await telemetry.startAgent({
      backend: "ax",
      model: "safe-model-id",
      inputBytes: Buffer.byteLength(payload),
    });
    await telemetry.observeEvent({
      type: "tool_start",
      id: "tool-1",
      name: "neko_graphjin_agent",
      input: { authorization: `Bearer ${secret}`, query: payload },
    });
    await telemetry.observeEvent({
      type: "tool_end",
      id: "tool-1",
      result: { token: secret, rows: [{ private: payload }] },
    });
    await telemetry.observeEvent({
      type: "message",
      role: "assistant",
      content: payload,
    });
    await telemetry.observeEvent({ type: "retry", reason: "empty_output" });
    await telemetry.observeEvent({
      type: "usage",
      source: "outer",
      provider: "provider-id",
      model: "resolved-model-id",
      usage: { totalTokens: 42, coverage: "complete" },
    });
    await telemetry.finishAgent({
      status: "ok",
      outputBytes: Buffer.byteLength(payload),
    });

    const encoded = JSON.stringify(sink.observations);
    expect(encoded).not.toContain(secret);
    expect(encoded).not.toContain(payload);
    expect(encoded).not.toContain("authorization");
    expect(encoded).not.toContain("query");
    expect(sink.observations.map((item) => item.kind)).toEqual(
      expect.arrayContaining([
        "model.request",
        "tool.start",
        "delegation.start",
        "model.response",
        "delegation.end",
        "tool.end",
        "run.first_output",
        "retry",
      ]),
    );
    expect(
      sink.observations.find((item) => item.kind === "tool.start")
        ?.measurements?.inputBytes,
    ).toBeGreaterThan(0);
    expect(
      sink.observations.find((item) => item.kind === "retry")?.attributes,
    ).toEqual({ "openneko.retry.reason": "empty_output" });
  });

  it("closes open model, delegation, tool, and stage spans on failure", async () => {
    const sink = new MemoryObservationSink();
    const observer = createHarnessObserver({ runId: "run-2", sinks: [sink] });
    const telemetry = createAgentEventTelemetry({
      observer,
      operationId: "workflow:run-2",
    });

    await telemetry.startAgent({ backend: "ax" });
    await telemetry.observeEvent({
      type: "tool_start",
      id: "tool-open",
      name: "neko_graphjin_agent",
      input: { value: "private" },
    });
    await telemetry.closeOpen({
      status: "error",
      outcome: "failed",
      errorType: "worker_interrupted",
      usageMissingReason: "model did not complete",
    });

    const endings = sink.observations.filter((item) =>
      ["tool.end", "delegation.end", "model.response", "stage.end"].includes(
        item.kind,
      ),
    );
    expect(endings).toHaveLength(5);
    expect(
      endings.filter((item) => item.kind === "model.response"),
    ).toHaveLength(2);
    expect(endings.every((item) => item.status === "error")).toBe(true);
  });

  it("records the backend failure code, message, and timeout on the agent stage", async () => {
    const sink = new MemoryObservationSink();
    const observer = createHarnessObserver({ runId: "run-3", sinks: [sink] });
    const telemetry = createAgentEventTelemetry({ observer, operationId: "work:run-3" });

    await telemetry.startAgent({ backend: "ax" });
    await telemetry.finishAgent({
      status: "error",
      errorType: "deadline_exceeded",
      errorCode: "deadline_exceeded",
      errorMessage: "ax turn exceeded its 300s budget",
      timedOut: true,
      degraded: true,
    });

    const stage = sink.observations.find((item) => item.kind === "stage.end");
    expect(stage).toMatchObject({
      status: "error",
      errorType: "deadline_exceeded",
      errorCode: "deadline_exceeded",
      errorMessage: "ax turn exceeded its 300s budget",
      attributes: { "openneko.stage": "agent", "openneko.timed_out": true, "openneko.outcome": "degraded" },
    });
  });
});
