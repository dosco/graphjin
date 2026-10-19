import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  ensureHostConfigProvisioned: vi.fn(async () => ({ modelHosts: [] })),
  claim: vi.fn(async () => true),
  complete: vi.fn(async () => undefined),
  link: vi.fn(async () => undefined),
  release: vi.fn(async () => undefined),
  prepare: vi.fn(),
  run: vi.fn(),
  claimApi: vi.fn(),
  loadPrepared: vi.fn(),
  finishApi: vi.fn(async () => undefined),
  persistApiTelemetry: vi.fn(async () => undefined),
  persistRunTelemetry: vi.fn(async () => undefined),
  runBatch: vi.fn(),
  updateProgress: vi.fn(async () => undefined),
  boundedResult: vi.fn((text: string) => ({ text, truncated: false })),
}));

vi.mock("@neko/llm", () => ({
  ensureHostConfigProvisioned: mocks.ensureHostConfigProvisioned,
  registerAgentCanceller: vi.fn(() => () => undefined),
}));

vi.mock("@neko/llm/spend", () => ({
  SpendBudgetExceeded: class SpendBudgetExceeded extends Error {},
  createRunSpendGuard: async (input: { emit: unknown; signal?: AbortSignal }) => ({
    emit: input.emit,
    signal: input.signal ?? new AbortController().signal,
    exceeded: () => null,
    dispose: () => {},
  }),
}));
vi.mock("@neko/llm/work", () => ({
  appendWorkRunEvent: vi.fn(async () => undefined),
  ensureAgentBroker: vi.fn(async () => ({})),
  registerAgentBrokerEventSink: vi.fn(() => () => undefined),
  scrubAgentEvent: vi.fn((_scrubber, event) => event),
  workflowRuntimeDepsFromConfig: vi.fn(() => ({})),
}));

vi.mock("@neko/llm/workflows", () => ({
  boundedWorkflowApiResult: mocks.boundedResult,
  claimWorkflowApiAdmission: mocks.claimApi,
  claimWorkflowScheduleFiring: mocks.claim,
  completeWorkflowScheduleFiring: mocks.complete,
  finishWorkflowApiAdmission: mocks.finishApi,
  linkWorkflowScheduleFiringRun: mocks.link,
  loadPreparedWorkflowRun: mocks.loadPrepared,
  persistWorkflowApiTelemetry: mocks.persistApiTelemetry,
  prepareWorkflowRun: mocks.prepare,
  releaseWorkflowScheduleFiringRun: mocks.release,
  runCompiledWorkflowApiBatch: mocks.runBatch,
  runWorkflowTurn: mocks.run,
  updateWorkflowApiRunProgress: mocks.updateProgress,
  WORKFLOW_API_INPUT_FILE: "trigger_input.json",
}));

vi.mock("../../src/secrets/instance.js", () => ({
  getCurrentScrubber: vi.fn(() => ({})),
}));

vi.mock("../../src/records/adapters.js", () => ({
  includeRecordActionDescriptors: vi.fn(() => []),
}));

vi.mock("../../src/telemetry.js", async () => {
  const actual = await vi.importActual<typeof import("../../src/telemetry.js")>(
    "../../src/telemetry.js",
  );
  return {
    ...actual,
    persistWorkflowRunTelemetry: mocks.persistRunTelemetry,
  };
});

import {
  runWorkflowRunFire,
  WorkflowRunInterrupted,
} from "../../src/jobs/workflow-run-fire";

const payload = {
  orgId: "org-test",
  workflowId: "f3ffcb96-d81e-422f-b81f-fbe08996287f",
  triggerKind: "cron" as const,
  scheduleFiringId: "5bc05029-f1ac-451d-b063-9736ec64ab93",
  triggerPayload: { firingTime: "2026-08-26T07:00:00.000Z" },
};

const prepared = {
  workflow: { id: payload.workflowId },
  workflowRun: {
    id: "workflow-run-1",
    createdAt: new Date("2026-08-26T07:00:00.000Z"),
  },
  threadId: "thread-1",
  workRunId: "work-run-1",
};

describe("workflow schedule firing consumer", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.claim.mockResolvedValue(true);
    mocks.prepare.mockResolvedValue(prepared);
    mocks.run.mockResolvedValue({ status: "completed" });
  });

  it("ignores duplicate deliveries before creating a workflow run", async () => {
    mocks.claim.mockResolvedValue(false);
    vi.spyOn(console, "log").mockImplementation(() => undefined);

    await runWorkflowRunFire(payload);

    expect(mocks.ensureHostConfigProvisioned).not.toHaveBeenCalled();
    expect(mocks.prepare).not.toHaveBeenCalled();
    expect(mocks.complete).not.toHaveBeenCalled();
  });

  it("links and completes the durable firing around one workflow run", async () => {
    await runWorkflowRunFire(payload);

    expect(mocks.claim).toHaveBeenCalledWith({
      firingId: payload.scheduleFiringId,
      orgId: payload.orgId,
      workflowId: payload.workflowId,
    });
    expect(mocks.link).toHaveBeenCalledWith(
      payload.scheduleFiringId,
      prepared.workflowRun.id,
    );
    expect(mocks.complete).toHaveBeenCalledWith(payload.scheduleFiringId);
    expect(mocks.release).not.toHaveBeenCalled();
    expect(mocks.persistRunTelemetry).toHaveBeenCalledWith(
      prepared.workflowRun.id,
      expect.objectContaining({ status: "completed", phases: expect.arrayContaining([expect.objectContaining({ name: "workflow.prepare", ok: true }), expect.objectContaining({ name: "config.provision", ok: true })]) }),
    );
  });

  it("releases an interrupted firing for at-least-once retry", async () => {
    mocks.run.mockResolvedValue({ status: "cancelled" });

    await expect(runWorkflowRunFire(payload)).rejects.toBeInstanceOf(
      WorkflowRunInterrupted,
    );
    expect(mocks.complete).not.toHaveBeenCalled();
    expect(mocks.release).toHaveBeenCalledWith(
      payload.scheduleFiringId,
      expect.any(WorkflowRunInterrupted),
    );
  });

  it("leaves a terminal linked run for recovery if ledger completion fails", async () => {
    mocks.complete.mockRejectedValueOnce(
      new Error("database connection reset"),
    );

    await expect(runWorkflowRunFire(payload)).rejects.toThrow(
      "database connection reset",
    );
    expect(mocks.release).not.toHaveBeenCalled();
  });
});

const apiPayload = {
  orgId: "org-test",
  workflowId: payload.workflowId,
  triggerKind: "api" as const,
  apiAdmissionId: "3a0fbec0-5566-4534-a482-d493835b49f7",
  workflowRunId: "workflow-run-api-1",
  workRunId: "work-run-api-1",
  executionMode: "single" as const,
};

const apiPrepared = {
  workflow: { id: payload.workflowId, orgId: "org-test" },
  workflowRun: {
    id: apiPayload.workflowRunId,
    triggerKind: "api",
    createdAt: new Date("2026-09-01T00:00:00.000Z"),
  },
  threadId: "thread-api-1",
  workRunId: apiPayload.workRunId,
};

const apiLimits = {
  requestLimitPerMinute: 30,
  pollLimitPerMinute: 120,
  queueCap: 25,
  concurrencyCap: 2,
  batchMaxRecords: 1000,
  batchChunkSize: 100,
  maxRequestBytes: 262144,
  maxResultBytes: 262144,
  maxArtifactBytes: 10485760,
  maxRuntimeSeconds: 600,
  maxModelCalls: 8,
  maxToolCalls: 32,
  maxTokensPerRun: 100000,
  maxCostMicrosPerRun: 5000000,
  rollingWindowSeconds: 86400,
  rollingTokenBudget: 250000,
  rollingCostMicrosBudget: 10000000,
  retentionHours: 168,
};

function apiClaim(overrides: Record<string, unknown> = {}) {
  return {
    action: "claimed" as const,
    id: apiPayload.apiAdmissionId,
    orgId: apiPayload.orgId,
    workflowId: apiPayload.workflowId,
    workflowRunId: apiPayload.workflowRunId,
    workRunId: apiPayload.workRunId,
    threadId: apiPrepared.threadId,
    mode: "single" as const,
    requestPayload: { orderId: "1042" },
    inputFilePath: null,
    batchContract: null,
    acceptedRecords: null,
    limits: apiLimits,
    admittedAt: new Date("2026-09-01T00:00:00.000Z"),
    attempt: 1,
    ...overrides,
  };
}

describe("workflow API execution consumer", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.claimApi.mockResolvedValue(apiClaim());
    mocks.loadPrepared.mockResolvedValue(apiPrepared);
    mocks.run.mockResolvedValue({
      status: "completed",
      finalText: '{"ok":true}',
    });
  });

  it("executes the pre-created canonical run and finalizes its bounded result", async () => {
    await runWorkflowRunFire(apiPayload);

    expect(mocks.prepare).not.toHaveBeenCalled();
    expect(mocks.loadPrepared).toHaveBeenCalledWith({
      orgId: apiPayload.orgId,
      workflowId: apiPayload.workflowId,
      workflowRunId: apiPayload.workflowRunId,
    });
    expect(mocks.run).toHaveBeenCalledWith(
      expect.objectContaining({
        prepared: apiPrepared,
        mode: "headless",
        userMessage: expect.stringContaining('"orderId":"1042"'),
        triggerInput: expect.objectContaining({ orderId: "1042" }),
        timeoutMs: apiLimits.maxRuntimeSeconds * 1_000,
      }),
      expect.any(Object),
    );
    expect(mocks.finishApi).toHaveBeenCalledWith(
      expect.objectContaining({
        admissionId: apiPayload.apiAdmissionId,
        workflowRunId: apiPayload.workflowRunId,
        status: "completed",
        terminalResult: { text: '{"ok":true}', truncated: false },
      }),
    );
    expect(mocks.persistApiTelemetry).toHaveBeenCalledWith(
      expect.objectContaining({ workflowRunId: apiPayload.workflowRunId }),
    );
  });

  it("ignores a duplicate durable delivery without another paid run", async () => {
    mocks.claimApi.mockResolvedValueOnce({ action: "duplicate" });
    vi.spyOn(console, "log").mockImplementation(() => undefined);

    await runWorkflowRunFire(apiPayload);

    expect(mocks.loadPrepared).not.toHaveBeenCalled();
    expect(mocks.run).not.toHaveBeenCalled();
    expect(mocks.finishApi).not.toHaveBeenCalled();
  });

  it("fails closed with the precise ceiling code before another tool/model turn", async () => {
    mocks.claimApi.mockResolvedValueOnce(
      apiClaim({ limits: { ...apiLimits, maxToolCalls: 1, maxModelCalls: 8 } }),
    );
    mocks.run.mockImplementationOnce(async (options) => {
      await options.emit({ type: "tool_start", id: "tool-1", name: "lookup" });
      await options.emit({ type: "tool_start", id: "tool-2", name: "lookup" });
      return { status: "completed", finalText: "should not finish" };
    });

    await expect(runWorkflowRunFire(apiPayload)).resolves.toBeUndefined();

    expect(mocks.finishApi).toHaveBeenCalledWith(
      expect.objectContaining({
        status: "failed",
        errorCode: "tool_call_limit",
      }),
    );
    expect(mocks.boundedResult).not.toHaveBeenCalled();
  });

  it("runs a compiled batch without invoking the model", async () => {
    const contract = {
      version: 1,
      compiled: true,
      compiler: "workflow" as const,
      recordsField: "records",
      columns: [{ name: "ID", path: "id" }],
    };
    mocks.claimApi.mockResolvedValueOnce(
      apiClaim({
        mode: "batch",
        requestPayload: null,
        inputFilePath: "runs/work-run-api-1/api-batch-input.ndjson",
        batchContract: contract,
        acceptedRecords: 2,
      }),
    );
    mocks.runBatch.mockResolvedValueOnce({
      artifactPath: "runs/work-run-api-1/artifacts/api-result.csv",
      progress: {
        stage: "completed",
        acceptedRows: 2,
        processedRows: 2,
        finalRows: 2,
        chunkCount: 1,
        artifactBytes: 12,
      },
    });

    await runWorkflowRunFire({ ...apiPayload, executionMode: "batch" });

    expect(mocks.run).not.toHaveBeenCalled();
    expect(mocks.runBatch).toHaveBeenCalledWith(
      expect.objectContaining({ acceptedRecords: 2, contract }),
    );
    expect(mocks.finishApi).toHaveBeenCalledWith(
      expect.objectContaining({
        status: "completed",
        artifactPath: "runs/work-run-api-1/artifacts/api-result.csv",
      }),
    );
  });
});
