import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { afterAll, afterEach, beforeEach, describe, expect, it } from "vitest";
import { NextRequest } from "next/server";
import { pool } from "@neko/db";
import { createTestOrg, dbReachable, deleteTestOrg, uniqueOrgId } from "@neko/db/test-helpers";
import { getReckonRun, importCompatWebhook, ulid } from "@neko/llm/workflows/compat";
import { getOrgAgentRoot } from "@neko/llm/work";

const TOKEN = "reckon-token-0123456789abcdef";
const reachable = await dbReachable();

type Handler = (request: NextRequest, context: { params: Promise<Record<string, string>> }) => Promise<Response>;

async function call(
  handler: Handler,
  input: {
    params: Record<string, string>;
    method?: "GET" | "POST";
    token?: string;
    idempotencyKey?: string;
    query?: string;
    body?: unknown;
    headers?: Record<string, string>;
  },
): Promise<{ status: number; body: unknown; headers: Headers }> {
  const request = new NextRequest(`http://localhost:3000/api/hooks/test${input.query ?? ""}`, {
    method: input.method ?? "POST",
    headers: {
      "content-type": "application/json",
      ...(input.token === undefined ? { "x-webhook-token": TOKEN } : input.token ? { "x-webhook-token": input.token } : {}),
      ...(input.idempotencyKey === undefined ? { "idempotency-key": "job-1" } : input.idempotencyKey ? { "idempotency-key": input.idempotencyKey } : {}),
      ...input.headers,
    },
    ...(input.body === undefined ? {} : { body: typeof input.body === "string" ? input.body : JSON.stringify(input.body) }),
  });
  const response = await handler(request, { params: Promise.resolve(input.params) });
  const text = await response.text();
  let parsed: unknown = text;
  try {
    parsed = text ? JSON.parse(text) : null;
  } catch {
    /* a streamed artifact is not JSON */
  }
  return { status: response.status, body: parsed, headers: response.headers };
}

(reachable ? describe : describe.skip)("reckon-compatible webhook routes", () => {
  let orgId: string;
  let reckonId: string;
  let POST: Handler;
  let RESULT: Handler;

  beforeEach(async () => {
    orgId = uniqueOrgId("reckon-hooks");
    reckonId = ulid();
    await createTestOrg(orgId);
    const { rows } = await pool().query<{ id: string }>(
      "insert into workflow_definition (org_id, name) values ($1, 'Account early warning') returning id",
      [orgId],
    );
    await importCompatWebhook({
      orgId,
      actorUserId: null,
      reckonWorkflowId: reckonId,
      workflowId: rows[0]!.id,
      token: TOKEN,
      params: ["account", "accounts"],
    });
    POST = (await import("@/app/api/hooks/[workflowId]/route")).POST as unknown as Handler;
    RESULT = (await import("@/app/api/hooks/[workflowId]/result/[runId]/route")).GET as unknown as Handler;
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
  });

  afterAll(async () => {
    await pool().end();
  });

  it("answers 202 with Reckon's pending body and replays one idempotency key", async () => {
    const accepted = await call(POST, { params: { workflowId: reckonId }, body: { account: "AC1", secret: "drop" } });
    expect(accepted.status).toBe(202);
    expect(accepted.headers.get("Retry-After")).toBe("10");
    expect(accepted.body).toMatchObject({
      ok: true,
      status: "queued",
      mode: "single",
      idempotentReplay: false,
      note: "Poll resultRequest.url with the same webhook token.",
      resultRequest: { method: "GET", headers: { "X-Webhook-Token": "<same webhook token used to start the run>" } },
    });
    const { runId, resultUrl } = accepted.body as { runId: string; resultUrl: string };
    expect(resultUrl).toBe(`/api/hooks/${reckonId}/result/${runId}`);

    const preview = await pool().query("select trigger_input_preview from workflow_run where org_id = $1", [orgId]);
    expect(preview.rows[0].trigger_input_preview).toEqual({ mode: "single", params: { account: "AC1" } });

    const replay = await call(POST, { params: { workflowId: reckonId }, body: { account: "AC1", secret: "drop" } });
    expect(replay.status).toBe(202);
    expect(replay.headers.get("Idempotency-Replayed")).toBe("true");
    expect(replay.body).toMatchObject({ runId, idempotentReplay: true });

    const conflict = await call(POST, { params: { workflowId: reckonId }, body: { account: "AC2" } });
    expect(conflict).toMatchObject({ status: 409, body: { error: "idempotency_conflict" } });
  });

  it("rejects an unknown webhook, a bad token and malformed requests in Reckon's order", async () => {
    expect(await call(POST, { params: { workflowId: ulid() }, body: {} })).toMatchObject({
      status: 404,
      body: { error: "not found" },
    });
    expect(await call(POST, { params: { workflowId: reckonId }, token: "wrong", body: {} })).toMatchObject({
      status: 401,
      body: { error: "unauthorized" },
    });
    expect(await call(POST, { params: { workflowId: reckonId }, idempotencyKey: "", body: {} })).toMatchObject({
      status: 400,
      body: { error: "invalid_idempotency_key" },
    });
    expect(await call(POST, { params: { workflowId: reckonId }, query: "?mode=bulk", body: {} })).toMatchObject({
      status: 400,
      body: { error: "invalid_mode", message: 'mode must be "single" or "batch"' },
    });
    expect(await call(POST, { params: { workflowId: reckonId }, body: "{oops" })).toMatchObject({
      status: 400,
      body: { error: "invalid_json" },
    });
    expect(await call(POST, { params: { workflowId: reckonId }, body: [1, 2] })).toMatchObject({
      status: 400,
      body: { error: "invalid_payload", message: "Request JSON must be an object." },
    });
    expect(
      await call(POST, {
        params: { workflowId: reckonId },
        headers: { "content-length": String(300 * 1024) },
        body: {},
      }),
    ).toMatchObject({ status: 413, body: { error: "payload_too_large", maxBytes: 262144 } });
    expect(
      await call(POST, {
        params: { workflowId: reckonId },
        query: "?mode=batch",
        body: { accounts: Array.from({ length: 1_001 }, (_, i) => `AC${i}`) },
      }),
    ).toMatchObject({ status: 413, body: { error: "too_many_records", maxRecords: 1_000 } });
  });

  it("serves the run result and Reckon's polling states", async () => {
    const accepted = await call(POST, { params: { workflowId: reckonId }, body: { account: "AC1" } });
    const { runId } = accepted.body as { runId: string };

    const queued = await call(RESULT, { params: { workflowId: reckonId, runId }, method: "GET" });
    expect(queued.status).toBe(202);
    expect(queued.headers.get("Retry-After")).toBe("10");
    expect(queued.body).toMatchObject({ ok: true, status: "queued", runId });

    expect(
      await call(RESULT, { params: { workflowId: reckonId, runId: ulid() }, method: "GET" }),
    ).toMatchObject({ status: 404, body: { error: "not found" } });
    expect(
      await call(RESULT, { params: { workflowId: reckonId, runId }, method: "GET", token: "wrong" }),
    ).toMatchObject({ status: 401 });

    const run = (await getReckonRun(reckonId, runId))!;
    const artifacts = join(getOrgAgentRoot(orgId), "runs", run.workRunId, "artifacts");
    await mkdir(artifacts, { recursive: true });
    await writeFile(join(artifacts, "result.json"), '{"ok":true,"band":"Dormant"}', "utf8");
    await pool().query("update workflow_run set status = 'completed' where org_id = $1", [orgId]);

    const served = await call(RESULT, { params: { workflowId: reckonId, runId }, method: "GET" });
    expect(served.status).toBe(200);
    expect(served.headers.get("Content-Type")).toBe("application/json; charset=utf-8");
    expect(served.headers.get("Content-Disposition")).toBe('inline; filename="result.json"');
    expect(served.headers.get("Cache-Control")).toBe("no-store");
    expect(served.body).toEqual({ ok: true, band: "Dormant" });

    await pool().query("update workflow_run set status = 'failed', error = 'agent failed' where org_id = $1", [orgId]);
    expect(await call(RESULT, { params: { workflowId: reckonId, runId }, method: "GET" })).toMatchObject({
      status: 502,
      body: { error: "run_failed", status: "error", message: "agent failed", progress: { phase: "failed" } },
    });

    await pool().query(
      "update workflow_run set status = 'completed', result_expires_at = now() - interval '1 hour' where org_id = $1",
      [orgId],
    );
    expect(await call(RESULT, { params: { workflowId: reckonId, runId }, method: "GET" })).toMatchObject({
      status: 410,
      body: { error: "artifact_expired", runId },
    });
  });
});
