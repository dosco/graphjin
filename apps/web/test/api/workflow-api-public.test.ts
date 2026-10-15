import { beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

const mocks = vi.hoisted(() => {
  class TestWorkflowApiError extends Error {
    constructor(
      readonly code: string,
      message: string,
      readonly status: number,
      readonly retryAfterSeconds?: number,
    ) {
      super(message);
    }
  }
  return {
    TestWorkflowApiError,
    edge: vi.fn(),
    admit: vi.fn(),
    status: vi.fn(),
    publicUrl: { value: null as string | null },
  };
});

vi.mock("@/lib/public-url-settings", () => ({
  withSavedPublicUrl: async (path: string) => `${mocks.publicUrl.value ?? ""}${path}`,
}));

vi.mock("@neko/llm/workflows", () => ({
  WORKFLOW_API_HARD_MAX_REQUEST_BYTES: 1024 * 1024,
  WorkflowApiError: mocks.TestWorkflowApiError,
  enforceWorkflowApiEdgeThrottle: mocks.edge,
  admitWorkflowApiRun: mocks.admit,
  getWorkflowApiRunStatus: mocks.status,
  parseWorkflowApiBearer: (value: string | null) =>
    value?.match(/^Bearer[ \t]+([^\s]+)$/i)?.[1] ?? null,
  workflowApiClientFingerprint: () => "client-fingerprint",
}));

import { POST } from "@/app/api/v1/workflows/[workflowId]/runs/route";
import { GET } from "@/app/api/v1/workflows/[workflowId]/runs/[runId]/route";

const postContext = {
  params: Promise.resolve({ workflowId: "workflow-a" }),
};
const getContext = {
  params: Promise.resolve({ workflowId: "workflow-a", runId: "run-a" }),
};

function postRequest(input: {
  url?: string;
  body?: string;
  contentType?: string;
  token?: string;
  idempotencyKey?: string;
} = {}): NextRequest {
  return new NextRequest(
    input.url ?? "http://localhost/api/v1/workflows/workflow-a/runs",
    {
      method: "POST",
      headers: {
        "content-type": input.contentType ?? "application/json",
        authorization: `Bearer ${input.token ?? "workflow-token"}`,
        "idempotency-key": input.idempotencyKey ?? "request-0001",
      },
      body: input.body ?? JSON.stringify({ orderId: "1042" }),
    },
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.publicUrl.value = null;
  mocks.edge.mockResolvedValue(undefined);
  mocks.admit.mockResolvedValue({
    runId: "run-a",
    status: "queued",
    mode: "single",
    replay: false,
    statusUrl: "/api/v1/workflows/workflow-a/runs/run-a",
    expiresAt: new Date("2026-09-08T00:00:00.000Z"),
  });
  mocks.status.mockResolvedValue({
    runId: "run-a",
    workflowId: "workflow-a",
    mode: "single",
    status: "running",
    createdAt: new Date("2026-09-01T00:00:00.000Z"),
    admittedAt: new Date("2026-09-01T00:00:01.000Z"),
    startedAt: new Date("2026-09-01T00:00:02.000Z"),
    finishedAt: null,
    expiresAt: new Date("2026-09-08T00:00:00.000Z"),
    progress: { stage: "running" },
    telemetry: null,
    result: null,
    artifact: null,
    error: null,
    retryAfterSeconds: 3,
  });
});

describe("public workflow API routes", () => {
  it("admits the default single mode asynchronously with the canonical run URL", async () => {
    const response = await POST(postRequest(), postContext);
    const body = await response.json();

    expect(response.status).toBe(202);
    expect(response.headers.get("location")).toBe(
      "/api/v1/workflows/workflow-a/runs/run-a",
    );
    expect(response.headers.get("retry-after")).toBe("3");
    expect(body).toMatchObject({ runId: "run-a", mode: "single" });
    expect(mocks.admit).toHaveBeenCalledWith({
      workflowId: "workflow-a",
      token: "workflow-token",
      idempotencyKey: "request-0001",
      mode: "single",
      value: { orderId: "1042" },
      clientFingerprint: "client-fingerprint",
    });
    expect(mocks.edge.mock.invocationCallOrder[0]).toBeLessThan(
      mocks.admit.mock.invocationCallOrder[0]!,
    );
  });

  it("starts run and artifact URLs with the saved public URL", async () => {
    mocks.publicUrl.value = "https://proxy.example.com/openneko";
    const admitted = await POST(postRequest(), postContext);
    const runUrl = "https://proxy.example.com/openneko/api/v1/workflows/workflow-a/runs/run-a";
    expect(admitted.headers.get("location")).toBe(runUrl);
    expect((await admitted.json()).statusUrl).toBe(runUrl);

    const running = await mocks.status();
    mocks.status.mockResolvedValueOnce({
      ...running,
      status: "completed",
      artifact: { url: "/api/v1/workflows/workflow-a/runs/run-a/artifact" },
    });
    const polled = await GET(
      new NextRequest(runUrl, { headers: { authorization: "Bearer workflow-token" } }),
      getContext,
    );
    expect((await polled.json()).artifact.url).toBe(`${runUrl}/artifact`);
  });

  it("keeps execution mode in the query string and admits batch explicitly", async () => {
    mocks.admit.mockResolvedValueOnce({
      runId: "run-b",
      status: "queued",
      mode: "batch",
      replay: false,
      statusUrl: "/api/v1/workflows/workflow-a/runs/run-b",
      expiresAt: new Date("2026-09-08T00:00:00.000Z"),
    });
    const response = await POST(
      postRequest({
        url: "http://localhost/api/v1/workflows/workflow-a/runs?mode=batch",
        body: JSON.stringify({ records: [{ id: 1 }] }),
      }),
      postContext,
    );

    expect(response.status).toBe(202);
    expect(mocks.admit).toHaveBeenCalledWith(
      expect.objectContaining({ mode: "batch", value: { records: [{ id: 1 }] } }),
    );
  });

  it("throttles malformed traffic before parsing or authenticating it", async () => {
    const response = await POST(
      postRequest({ contentType: "text/plain", body: "not-json" }),
      postContext,
    );

    expect(response.status).toBe(415);
    expect(mocks.edge).toHaveBeenCalledWith("client-fingerprint");
    expect(mocks.admit).not.toHaveBeenCalled();
  });

  it("rejects chunked bodies over the hard cap without calling admission", async () => {
    const request = postRequest({
      body: JSON.stringify({ value: "x".repeat(1024 * 1024) }),
    });
    request.headers.delete("content-length");
    const response = await POST(request, postContext);
    const body = await response.json();

    expect(response.status).toBe(413);
    expect(body).toMatchObject({ error: { code: "request_too_large" } });
    expect(mocks.admit).not.toHaveBeenCalled();
  });

  it("uses the same generic credential error for hidden or inaccessible workflows", async () => {
    mocks.admit.mockRejectedValueOnce(
      new mocks.TestWorkflowApiError(
        "invalid_credentials",
        "The workflow credential is invalid.",
        401,
      ),
    );
    const response = await POST(postRequest({ token: "wrong" }), postContext);
    const body = await response.json();

    expect(response.status).toBe(401);
    expect(response.headers.get("www-authenticate")).toContain("Bearer");
    expect(body).toEqual({
      error: {
        code: "invalid_credentials",
        message: "The workflow credential is invalid.",
      },
    });
  });

  it("polls with the same credential and exposes Retry-After while active", async () => {
    const request = new NextRequest(
      "http://localhost/api/v1/workflows/workflow-a/runs/run-a",
      { headers: { authorization: "Bearer workflow-token" } },
    );
    const response = await GET(request, getContext);
    const body = await response.json();

    expect(response.status).toBe(200);
    expect(response.headers.get("retry-after")).toBe("3");
    expect(body).toMatchObject({ runId: "run-a", status: "running" });
    expect(mocks.status).toHaveBeenCalledWith({
      workflowId: "workflow-a",
      runId: "run-a",
      token: "workflow-token",
      clientFingerprint: "client-fingerprint",
    });
  });
});

it("correlates admission phases without exposing credentials or input", async () => {
  const log = vi.spyOn(console, "log").mockImplementation(() => {});
  try {
    await POST(postRequest(), postContext);
    const entries = log.mock.calls.map(call => JSON.parse(String(call[0])));
    expect(entries).toEqual(expect.arrayContaining([expect.objectContaining({ phase: "workflow.api_admitted", workflowRunId: "run-a", replay: false }), expect.objectContaining({ phase: "workflow.api_admit", ok: true, durationMs: expect.any(Number) })]));
    expect(new Set(entries.map(entry => entry.requestId)).size).toBe(1);
    expect(JSON.stringify(entries)).not.toMatch(/workflow-token|request-0001|orderId/);
  } finally { log.mockRestore(); }
});
