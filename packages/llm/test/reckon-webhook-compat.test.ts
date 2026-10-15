import { mkdir, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { pool } from "@neko/db";
import {
  admitReckonWebhookRun,
  buildReckonSeedMessage,
  consumeReckonStartRate,
  filterTriggerParams,
  getCompatWebhook,
  getReckonRun,
  importCompatWebhook,
  largestArrayLength,
  parseReckonIdempotencyKey,
  parseReckonMode,
  pickReckonArtifact,
  reckonRequestFingerprint,
  reckonRunStatus,
  reckonSeedMessageFrom,
  reckonTokenMatches,
  ReckonWebhookError,
  removeCompatWebhook,
  sha256Hex,
  ulid,
} from "../src/workflows/compat";
import { getOrgAgentRoot } from "../src/work/workspace";

async function dbReachable(): Promise<boolean> {
  try {
    await pool().query("select 1 from compat_webhook limit 1");
    return true;
  } catch {
    return false;
  }
}

const describeIfDb = (await dbReachable()) ? describe : describe.skip;
const TOKEN = "reckon-token-0123456789abcdef";

async function withWebhook<T>(
  fn: (context: { orgId: string; workflowId: string; reckonId: string }) => Promise<T>,
): Promise<T> {
  const orgId = `reckon-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
  const reckonId = ulid();
  await pool().query("insert into organization (id, name) values ($1, 'Reckon compat test')", [orgId]);
  try {
    const { rows } = await pool().query<{ id: string }>(
      "insert into workflow_definition (org_id, name) values ($1, 'Daily lead union') returning id",
      [orgId],
    );
    const workflowId = rows[0]!.id;
    await importCompatWebhook({
      orgId,
      actorUserId: null,
      reckonWorkflowId: reckonId,
      workflowId,
      token: TOKEN,
      params: ["date"],
    });
    return await fn({ orgId, workflowId, reckonId });
  } finally {
    await pool().query("delete from organization where id = $1", [orgId]);
  }
}

describe("reckon webhook contract", () => {
  it("parses Reckon's idempotency keys and modes", () => {
    expect(parseReckonIdempotencyKey("run-1.2:3_4")).toBe("run-1.2:3_4");
    expect(parseReckonIdempotencyKey("a")).toBe("a");
    expect(parseReckonIdempotencyKey("")).toBeNull();
    expect(parseReckonIdempotencyKey("has space")).toBeNull();
    expect(parseReckonIdempotencyKey("x".repeat(201))).toBeNull();
    expect(parseReckonMode(null)).toBe("single");
    expect(parseReckonMode("batch")).toBe("batch");
    expect(parseReckonMode("bulk")).toBeNull();
  });

  it("counts the largest array anywhere in the body and filters to the allowlist", () => {
    expect(largestArrayLength({ a: [1, 2], b: { c: [1, 2, 3] } })).toBe(3);
    expect(filterTriggerParams({ date: "2026-09-15", secret: "x" }, ["date"])).toEqual({ date: "2026-09-15" });
    expect(filterTriggerParams({ date: "1", other: "2" }, null)).toEqual({ date: "1", other: "2" });
  });

  it("fingerprints a request by mode and canonical params", () => {
    const one = reckonRequestFingerprint("batch", { b: 2, a: [1, { y: 1, x: 2 }] });
    const two = reckonRequestFingerprint("batch", { a: [1, { x: 2, y: 1 }], b: 2 });
    expect(one).toBe(two);
    expect(reckonRequestFingerprint("single", { b: 2, a: [1, { y: 1, x: 2 }] })).not.toBe(one);
  });

  it("compares tokens by digest and mints ULIDs", () => {
    expect(reckonTokenMatches(TOKEN, sha256Hex(TOKEN))).toBe(true);
    expect(reckonTokenMatches("wrong", sha256Hex(TOKEN))).toBe(false);
    const id = ulid(1_758_000_000_000);
    expect(id).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);
    expect(ulid(1_758_000_000_000) < ulid(1_758_000_001_000)).toBe(true);
  });

  it("maps OpenNeko run status onto Reckon's vocabulary", () => {
    expect(["queued", "running", "completed", "cancelled", "needs_input", "failed"].map(reckonRunStatus)).toEqual([
      "queued",
      "running",
      "completed",
      "aborted",
      "needs_input",
      "error",
    ]);
  });

  it("builds a start message the agent can follow", () => {
    const message = buildReckonSeedMessage({
      mode: "batch",
      params: { accounts: ["A", "B"] },
      startedAt: new Date("2026-09-18T07:00:00Z"),
      batchChunkSize: 250,
    });
    expect(message).toContain("[webhook run started at 2026-09-18T07:00:00.000Z]");
    expect(message).toContain('"accounts"');
    expect(message).toContain("$ARTIFACT_DIR/result.csv");
    expect(message).toContain("chunks of at most 250 records");
    expect(message).toContain("$RUN_DIR/trigger_input.json");
    expect(reckonSeedMessageFrom({ openneko_reckon_webhook: { message } })).toBe(message);
    expect(reckonSeedMessageFrom({ other: 1 })).toBeNull();
  });
});

describeIfDb("reckon webhook admission", () => {
  it("imports a webhook, hides the token, and enables API access with compat limits", async () => {
    await withWebhook(async ({ orgId, reckonId, workflowId }) => {
      const webhook = await getCompatWebhook(reckonId);
      expect(webhook).toMatchObject({ orgId, workflowId, params: ["date"], batchChunkSize: 1_000 });
      expect(webhook!.tokenSha256).toBe(sha256Hex(TOKEN));
      const access = await pool().query(
        "select enabled, max_tokens_per_run, max_runtime_seconds, max_tool_calls from workflow_api_access where workflow_id = $1",
        [workflowId],
      );
      expect(access.rows[0]).toEqual({
        enabled: true,
        max_tokens_per_run: 3_000_000,
        max_runtime_seconds: 1_800,
        max_tool_calls: 1_024,
      });
      expect(await removeCompatWebhook({ orgId, actorUserId: null, reckonWorkflowId: reckonId })).toBe(true);
      expect(await getCompatWebhook(reckonId)).toBeNull();
    });
  });

  it("admits a run the dispatcher can pick up, and replays one idempotency key", async () => {
    await withWebhook(async ({ orgId, reckonId, workflowId }) => {
      const webhook = (await getCompatWebhook(reckonId))!;
      const admitted = await admitReckonWebhookRun({
        webhook,
        mode: "batch",
        params: { date: "2026-09-15" },
        idempotencyKey: "job-1",
        requestBytes: 42,
      });
      expect(admitted).toMatchObject({ status: "queued", mode: "batch", replay: false });
      expect(admitted.runId).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);

      const admission = await pool().query(
        `select admission.status, admission.execution_mode, admission.request_payload, run.trigger_kind, run.status as run_status
           from workflow_api_admission admission
           join workflow_run run on run.id = admission.workflow_run_id
          where admission.workflow_id = $1`,
        [workflowId],
      );
      expect(admission.rows[0]).toMatchObject({ status: "pending", execution_mode: "single", trigger_kind: "api", run_status: "queued" });
      expect(reckonSeedMessageFrom(admission.rows[0].request_payload)).toContain("$ARTIFACT_DIR/result.csv");

      const reservation = await pool().query(
        "select source, reserved_micros from spend_reservation where org_id = $1",
        [orgId],
      );
      expect(reservation.rows[0]).toMatchObject({ source: "webhook" });

      const replay = await admitReckonWebhookRun({
        webhook,
        mode: "batch",
        params: { date: "2026-09-15" },
        idempotencyKey: "job-1",
        requestBytes: 42,
      });
      expect(replay).toMatchObject({ runId: admitted.runId, replay: true });

      await expect(
        admitReckonWebhookRun({
          webhook,
          mode: "batch",
          params: { date: "2026-09-16" },
          idempotencyKey: "job-1",
          requestBytes: 42,
        }),
      ).rejects.toMatchObject({ status: 409, body: { error: "idempotency_conflict" } });
    });
  });

  it("maps a full spend budget onto Reckon's budget reason", async () => {
    await withWebhook(async ({ orgId, reckonId }) => {
      await pool().query(
        "update spend_limit set org_hourly_micros = 4000000 where org_id = $1 and workflow_id is null",
        [orgId],
      );
      const webhook = (await getCompatWebhook(reckonId))!;
      const rejected = await admitReckonWebhookRun({
        webhook,
        mode: "single",
        params: {},
        idempotencyKey: "job-budget",
        requestBytes: 2,
      }).catch((error) => error);
      expect(rejected).toBeInstanceOf(ReckonWebhookError);
      expect(rejected).toMatchObject({
        status: 429,
        body: { error: "global_hourly_budget", limitUsd: 4, reservationUsd: 5 },
      });
      const alerts = await pool().query("select kind from behavior_alert where org_id = $1", [orgId]);
      expect(alerts.rows).toEqual([{ kind: "spend.budget_blocked" }]);
    });
  });

  it("refuses more than ten starts a minute for one webhook", async () => {
    await withWebhook(async ({ reckonId }) => {
      const webhook = (await getCompatWebhook(reckonId))!;
      const now = new Date("2026-09-18T07:30:10Z");
      // The bucket rows outlive the test org, so start this window clean.
      await pool().query("delete from workflow_api_rate_bucket where scope_id in ($1, $2)", [
        "reckon:global",
        `reckon:${reckonId}`,
      ]);
      for (let i = 0; i < 10; i += 1) {
        await consumeReckonStartRate(webhook, now);
      }
      const rejected = await consumeReckonStartRate(webhook, now).catch((error) => error);
      expect(rejected).toMatchObject({ status: 429, body: { error: "workflow_rate" }, retryAfterSeconds: 50 });
    });
  });
});

describeIfDb("reckon webhook results", () => {
  async function artifact(orgId: string, workRunId: string, name: string, body: string) {
    const root = join(getOrgAgentRoot(orgId), "runs", workRunId, "artifacts");
    await mkdir(root, { recursive: true });
    await writeFile(join(root, name), body, "utf8");
  }

  it("serves result.json for a single run and result.csv for a batch run", async () => {
    await withWebhook(async ({ orgId, reckonId }) => {
      const webhook = (await getCompatWebhook(reckonId))!;
      const single = await admitReckonWebhookRun({ webhook, mode: "single", params: {}, idempotencyKey: "s1", requestBytes: 2 });
      const batch = await admitReckonWebhookRun({ webhook, mode: "batch", params: {}, idempotencyKey: "b1", requestBytes: 2 });
      await pool().query("update workflow_run set status = 'completed' where org_id = $1", [orgId]);

      const singleRun = (await getReckonRun(reckonId, single.runId))!;
      const batchRun = (await getReckonRun(reckonId, batch.runId))!;
      expect(singleRun.status).toBe("completed");

      await artifact(orgId, singleRun.workRunId, "result.json", '{"ok":true}');
      await artifact(orgId, singleRun.workRunId, "scratch.json", "{}");
      expect(await pickReckonArtifact(singleRun)).toMatchObject({
        ok: true,
        name: "result.json",
        contentType: "application/json; charset=utf-8",
      });

      expect(await pickReckonArtifact(batchRun)).toMatchObject({ ok: false, reason: "missing_batch_result" });
      await artifact(orgId, batchRun.workRunId, "result.csv", "a,b\n1,2\n");
      expect(await pickReckonArtifact(batchRun)).toMatchObject({
        ok: true,
        name: "result.csv",
        contentType: "text/csv; charset=utf-8",
      });
    });
  });

  it("reports an ambiguous artifact set and an expired result", async () => {
    await withWebhook(async ({ orgId, reckonId }) => {
      const webhook = (await getCompatWebhook(reckonId))!;
      const admitted = await admitReckonWebhookRun({ webhook, mode: "single", params: {}, idempotencyKey: "s2", requestBytes: 2 });
      await pool().query("update workflow_run set status = 'completed' where org_id = $1", [orgId]);
      const run = (await getReckonRun(reckonId, admitted.runId))!;
      await artifact(orgId, run.workRunId, "one.csv", "a\n");
      await artifact(orgId, run.workRunId, "two.csv", "b\n");
      expect(await pickReckonArtifact(run)).toMatchObject({ ok: false, reason: "ambiguous", names: ["one.csv", "two.csv"] });

      await pool().query("update workflow_run set result_expires_at = now() - interval '1 hour' where org_id = $1", [orgId]);
      expect((await getReckonRun(reckonId, admitted.runId))!.expired).toBe(true);
    });
  });
});
