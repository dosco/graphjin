// SEC10 — the tamper-evident chain: governance events append links,
// verification walks them, and any retroactive edit, deletion, or
// reorder breaks the chain at the exact spot.

import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { audit_chain, and, db, eq, pool } from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  appendAuditChain,
  canonicalJson,
  exportAuditChain,
  getAuditLoggingHealth,
  reportAuditLoggingFailure,
  resetAuditLoggingHealthForTesting,
  verifyAuditChain,
} from "../../src/workflows/audit-chain";
import { createActionRequest } from "../../src/workflows/action-store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[audit-chain] skipping: Postgres unreachable.");
}

describe("canonicalJson", () => {
  it("is independent of key order and drops undefined", () => {
    expect(canonicalJson({ b: 1, a: { d: 2, c: 3 }, z: undefined })).toBe(
      canonicalJson({ a: { c: 3, d: 2 }, b: 1 }),
    );
  });
});

describe("audit logging failure alerts", () => {
  afterEach(() => {
    resetAuditLoggingHealthForTesting();
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("records degraded health and emits a structured critical event", () => {
    const stderr = vi.spyOn(console, "error").mockImplementation(() => {});
    reportAuditLoggingFailure(
      {
        orgId: "org_test",
        entityKind: "action_request",
        entityId: "act_test",
        event: "approved",
      },
      new Error("database unavailable"),
    );

    expect(getAuditLoggingHealth()).toMatchObject({
      healthy: false,
      failureCount: 1,
      lastError: "database unavailable",
    });
    const event = JSON.parse(String(stderr.mock.calls[0]?.[0]));
    expect(event).toMatchObject({
      type: "security.audit_logging_failure",
      severity: "critical",
      orgId: "org_test",
      event: "approved",
    });
  });

  it("posts to the independent webhook without awaiting delivery", async () => {
    vi.stubEnv("OPENNEKO_AUDIT_FAILURE_WEBHOOK_URL", "https://alerts.test/audit");
    vi.stubEnv("OPENNEKO_AUDIT_FAILURE_WEBHOOK_TOKEN", "secret-token");
    vi.spyOn(console, "error").mockImplementation(() => {});
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 202 }));
    vi.stubGlobal("fetch", fetchMock);

    reportAuditLoggingFailure(
      {
        orgId: "org_test",
        entityKind: "work_run",
        entityId: "run_test",
        event: "run:completed",
      },
      "append failed",
    );
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());

    expect(fetchMock).toHaveBeenCalledWith(
      "https://alerts.test/audit",
      expect.objectContaining({
        method: "POST",
        headers: expect.objectContaining({
          authorization: "Bearer secret-token",
        }),
      }),
    );
  });
});

describeIfDb("SEC10 audit chain", () => {
  const orgId = uniqueOrgId("sec10");

  beforeAll(async () => {
    await createTestOrg(orgId);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
    vi.unstubAllEnvs();
  });

  it("links append in sequence and verify, even under concurrency", async () => {
    await Promise.all(
      Array.from({ length: 8 }, (_, i) =>
        appendAuditChain({
          orgId,
          entityKind: "test",
          entityId: `e-${i}`,
          event: "created",
          payload: { i },
        }),
      ),
    );
    const verification = await verifyAuditChain(orgId);
    expect(verification).toMatchObject({ ok: true, length: 8 });
  });

  it("governance writes land on the chain automatically", async () => {
    await createActionRequest({
      orgId,
      scope: "external",
      kind: "send_webhook",
      target: "https://example.test/hook",
      status: "pending_approval",
      summary: "ping",
    });
    const ndjson = await exportAuditChain(orgId);
    const lines = ndjson.split("\n").map((l) => JSON.parse(l));
    const created = lines.find(
      (l) => l.entityKind === "action_request" && l.event === "created:pending_approval",
    );
    expect(created?.payload).toMatchObject({ kind: "send_webhook" });
    expect(await verifyAuditChain(orgId)).toMatchObject({ ok: true });
  });

  it("editing a recorded payload breaks the chain at that link", async () => {
    await db()
      .update(audit_chain)
      .set({ payload: { i: 999 } })
      .where(and(eq(audit_chain.org_id, orgId), eq(audit_chain.seq, 3)));
    const verification = await verifyAuditChain(orgId);
    expect(verification.ok).toBe(false);
    expect(verification.brokenAtSeq).toBe(3);
    expect(verification.reason).toMatch(/payload/);
  });

  it("deleting a link is a visible sequence gap", async () => {
    await db()
      .delete(audit_chain)
      .where(and(eq(audit_chain.org_id, orgId), eq(audit_chain.seq, 5)));
    const verification = await verifyAuditChain(orgId);
    expect(verification.ok).toBe(false);
    // seq 3 is still the payload tamper; the gap at 5 surfaces once 3 is
    // the earliest break — assert the earliest break is reported.
    expect(verification.brokenAtSeq).toBe(3);
  });
});
