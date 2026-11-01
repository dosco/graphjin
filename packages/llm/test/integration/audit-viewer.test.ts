// ADM4 — the audit trail is admin-gated on the REQUESTING run's K1
// actor, server-side; an admin run sees requests with their SEC5 dual
// identity plus SEC7 alerts.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { behavior_alert, db, pool } from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { createActionRequest } from "../../src/workflows/action-store";
import { inProcessControlPlane } from "../../src/work";
import { createWorkRun, createWorkThread } from "../../src/work/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[audit-viewer] skipping: Postgres unreachable.");
}

describeIfDb("ADM4 audit viewer", () => {
  const orgId = uniqueOrgId("adm4");
  let adminRunId: string;
  let memberRunId: string;

  beforeAll(async () => {
    await createTestOrg(orgId);
    const thread = await createWorkThread(orgId, "t", "web");
    adminRunId = (
      await createWorkRun(orgId, thread.id, "ax", { userId: null, role: "admin" })
    ).id;
    memberRunId = (
      await createWorkRun(orgId, thread.id, "ax", { userId: null, role: "member" })
    ).id;
    await createActionRequest({
      orgId,
      scope: "internal",
      kind: "user_admin",
      target: "x",
      status: "pending_approval",
      summary: "invite someone",
      workRunId: adminRunId,
    });
    await db().insert(behavior_alert).values({
      org_id: orgId,
      kind: "memory_write_volume",
      subject: orgId,
      observed: 120,
      threshold: 100,
      window_seconds: 3600,
    });
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  it("member and run-less callers are denied", async () => {
    expect(
      await inProcessControlPlane.listAuditTrail({ orgId, runId: memberRunId }),
    ).toEqual({ denied: true });
    expect(await inProcessControlPlane.listAuditTrail({ orgId })).toEqual({
      denied: true,
    });
  });

  it("an admin run sees the trail with dual identity + alerts", async () => {
    const result = await inProcessControlPlane.listAuditTrail({
      orgId,
      runId: adminRunId,
    });
    expect(result.denied).toBeUndefined();
    const request = result.requests?.find((r) => r.kind === "user_admin");
    expect(request).toMatchObject({
      actorRole: "admin",
      actorBackend: "ax",
      status: "pending_approval",
    });
    expect(result.alerts?.some((a) => a.kind === "memory_write_volume")).toBe(true);
  });
});
