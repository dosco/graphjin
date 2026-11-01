import { describe, expect, it } from "vitest";
import {
  HarnessRunSummaryAccumulator,
  MemoryObservationSink,
  createHarnessObserver,
} from "@neko/telemetry";
import type { AgentBackend, AgentRunResult } from "../src/agent-backend";
import { observeAgentJob } from "../src/agent-job-telemetry";

function backendReturning(result: AgentRunResult): AgentBackend {
  return {
    id: "ax",
    model: "model-a",
    capabilities: { mcpTools: true, sessionResume: false },
    run: async () => result,
  };
}

describe("observeAgentJob", () => {
  it("pairs run.start and run.end for a completed job", async () => {
    const sink = new MemoryObservationSink();
    const observer = createHarnessObserver({ runId: "job-1", sinks: [sink] });
    const value = await observeAgentJob(
      { observer, operationId: "profiler:job-1", productPath: "profiler" },
      async (observed) =>
        observed(backendReturning({ finalText: "ok", status: "completed" })).run({ prompt: "p" }),
    );
    expect(value.finalText).toBe("ok");
    expect(sink.observations.map((item) => [item.kind, item.status])).toEqual([
      ["run.start", "unset"],
      ["run.end", "ok"],
    ]);
    expect(sink.observations[1]?.attributes).toMatchObject({
      "openneko.backend": "ax",
      "gen_ai.request.model": "model-a",
      "openneko.outcome": "completed",
    });
  });

  it("ends a failed job with the backend code and message", async () => {
    const sink = new MemoryObservationSink();
    const summary = new HarnessRunSummaryAccumulator("job-2");
    const observer = createHarnessObserver({ runId: "job-2", sinks: [sink, summary] });
    await expect(
      observeAgentJob(
        { observer, operationId: "bootstrap-metrics:job-2", productPath: "bootstrap_metrics" },
        async (observed) => {
          const result = await observed(
            backendReturning({
              finalText: "",
              status: "failed",
              error: "ax turn exceeded its 60s budget",
              errorCode: "deadline_exceeded",
              timedOut: true,
            }),
          ).run({ prompt: "p" });
          throw new Error(result.error);
        },
      ),
    ).rejects.toThrow("ax turn exceeded its 60s budget");
    expect(sink.observations.at(-1)).toMatchObject({
      kind: "run.end",
      status: "error",
      errorType: "deadline_exceeded",
      errorCode: "deadline_exceeded",
      errorMessage: "ax turn exceeded its 60s budget",
      attributes: { "openneko.outcome": "failed", "openneko.timed_out": true },
    });
    expect(summary.snapshot()).toMatchObject({
      status: "failed",
      productPath: "bootstrap_metrics",
      errorCode: "deadline_exceeded",
    });
  });
});
