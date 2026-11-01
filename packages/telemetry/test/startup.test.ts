import { afterEach, expect, it, vi } from "vitest";
import { createHarnessObserver, HarnessRunSummaryAccumulator, MemoryObservationSink } from "../src";
import { bindStartupRun, failStartupRun, startupEvent, startupPhase, withStartupTrace } from "../src/startup";
afterEach(() => vi.restoreAllMocks());
it("links preflight phases, pairs failures, persists durations, and redacts values", async () => {
  const log = vi.spyOn(console, "log").mockImplementation(() => {});
  const sink = new MemoryObservationSink();
  const summary = new HarnessRunSummaryAccumulator("r");
  const observer = createHarnessObserver({ runId: "r", sinks: [sink, summary] });
  await withStartupTrace({ requestId: "q", threadId: "t" }, async () => {
    await startupPhase("identity", async () => 42);
    await bindStartupRun("r", observer);
    await expect(startupPhase("broken", async () => { throw new Error("original"); })).rejects.toThrow("original");
    startupEvent("cache", { outcome: "hit", authorization: "private", prompt: "private" });
  });
  expect(sink.observations.map(o => [o.kind, o.parentOperationId])).toEqual([
    ["stage.start", "work:r"], ["stage.end", "work:r"], ["stage.start", "work:r"], ["stage.end", "work:r"],
  ]);
  expect(summary.snapshot().phases).toEqual([
    { name: "identity", durationMs: expect.any(Number), ok: true },
    { name: "broken", durationMs: expect.any(Number), ok: false },
  ]);
  expect(sink.observations[3]).toMatchObject({ status: "error", errorType: "Error", errorMessage: "original" });
  const event = JSON.parse(log.mock.calls.at(-1)![0]);
  expect(event).toMatchObject({ requestId: "q", threadId: "t", runId: "r", outcome: "hit" });
  expect(event).not.toHaveProperty("authorization");
  expect(event).not.toHaveProperty("prompt");
});
it("isolates overlapping runs and tolerates a failed observer/logger", async () => {
  const log = vi.spyOn(console, "log").mockImplementation(() => {});
  await Promise.all(["a", "b"].map(runId => withStartupTrace({ runId }, async () => {
    await startupPhase("outer", async () => { await Promise.resolve(); startupEvent("inside", {}); });
  })));
  expect(log.mock.calls.map(call => JSON.parse(call[0])).filter(e => e.phase === "inside").map(e => e.runId).sort()).toEqual(["a", "b"]);
  log.mockImplementation(() => { throw new Error("log down"); });
  const observer = { observe: vi.fn().mockRejectedValue(new Error("sink down")), flush: async () => {}, shutdown: async () => {} };
  await expect(withStartupTrace({ runId: "c", observer }, () => startupPhase("safe", async () => 7))).resolves.toBe(7);
});

it("keeps the enclosing HTTP phase linked when the run is created inside it", async () => {
  vi.spyOn(console, "log").mockImplementation(() => {});
  const sink = new MemoryObservationSink();
  const observer = createHarnessObserver({ runId: "r", sinks: [sink] });
  await withStartupTrace({ requestId: "q" }, () => startupPhase("http.submit", async () => { await bindStartupRun("r", observer); }));
  expect(sink.observations.map(o => [o.kind, o.parentOperationId])).toEqual([["stage.start", "work:r"], ["stage.end", "work:r"]]);
});

it("attaches workflow preflight phases to the workflow root", async () => {
  vi.spyOn(console, "log").mockImplementation(() => {});
  const sink = new MemoryObservationSink();
  const observer = createHarnessObserver({ runId: "r", sinks: [sink] });
  await withStartupTrace({ requestId: "q" }, async () => {
    await startupPhase("prepare", async () => 1);
    await bindStartupRun("r", observer, "workflow:r");
    await startupPhase("ready", async () => 2);
  });
  expect(sink.observations.every(o => o.parentOperationId === "workflow:r")).toBe(true);
});

it("ends a request that failed before its run existed as a failed run with its phases", async () => {
  vi.spyOn(console, "log").mockImplementation(() => {});
  const sink = new MemoryObservationSink();
  const summary = new HarnessRunSummaryAccumulator("q");
  const created: string[] = [];
  await withStartupTrace({ requestId: "q" }, async () => {
    const error = await startupPhase("http.submit", () =>
      startupPhase("config.provision", async () => { throw new Error("gateway sync failed key=sk-abcdefghijklmnop"); }),
    ).catch((e: unknown) => e);
    await failStartupRun(error, (runId) => {
      created.push(runId);
      return createHarnessObserver({ runId, sinks: [sink, summary] });
    }, { "openneko.product.path": "work" });
  });
  expect(created).toEqual(["q"]);
  expect(sink.observations.map(o => o.kind)).toEqual(["run.start", "stage.start", "stage.start", "stage.end", "stage.end", "run.end"]);
  expect(sink.observations.at(-1)).toMatchObject({
    operationId: "work:q",
    status: "error",
    errorCode: "startup.config.provision",
    errorMessage: "gateway sync failed key=[REDACTED]",
  });
  expect(summary.snapshot()).toMatchObject({ status: "failed", productPath: "work", errorCode: "startup.config.provision" });
});

it("ends a bound run on its own observer when a later startup step fails", async () => {
  vi.spyOn(console, "log").mockImplementation(() => {});
  const sink = new MemoryObservationSink();
  const observer = createHarnessObserver({ runId: "r", sinks: [sink] });
  const create = vi.fn();
  await withStartupTrace({ requestId: "q" }, async () => {
    await bindStartupRun("r", observer, "workflow:r");
    await failStartupRun(new Error("save failed"), create);
  });
  expect(create).not.toHaveBeenCalled();
  expect(sink.observations.at(-1)).toMatchObject({ kind: "run.end", operationId: "workflow:r", errorCode: "startup_failed", errorMessage: "save failed" });
});
