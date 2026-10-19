// SEC5 — dual-identity audit: every action request snapshots the human
// principal (K1 actor) AND the agent backend at creation, and every
// authenticated broker call lands a control_plane_audit row with the
// same dual identity.

import { afterAll, describe, expect, it, vi } from "vitest";
import {
  app_user,
  control_plane_audit,
  db,
  eq,
  pool,
} from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { createActionRequest } from "../../src/workflows/action-store";
import {
  inProcessControlPlane,
  startAgentBroker,
} from "../../src/work";
import { createWorkRun, createWorkThread } from "../../src/work/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[dual-identity-audit] skipping: Postgres unreachable.");
}

describeIfDb("SEC5 dual-identity audit", () => {
  afterAll(async () => {
    await pool().end();
    vi.unstubAllEnvs();
  });

  it("createActionRequest snapshots the run's actor + backend", async () => {
    const orgId = uniqueOrgId("sec5");
    await createTestOrg(orgId);
    try {
      const ada = `${orgId}-ada`;
      await db().insert(app_user).values({
        id: ada,
        email: "ada@example.com",
        org_id: orgId,
      });
      const thread = await createWorkThread(orgId, "t", "web", ada);
      const run = await createWorkRun(orgId, thread.id, "hermes", {
        userId: ada,
        role: "member",
      });
      const request = await createActionRequest({
        orgId,
        scope: "internal",
        kind: "user_admin",
        target: "x",
        status: "pending_approval",
        summary: "test",
        workRunId: run.id,
      });
      expect(request.actorUserId).toBe(ada);
      expect(request.actorRole).toBe("member");
      expect(request.actorBackend).toBe("hermes");

      // No run context → identity stays null rather than inventing one.
      const bare = await createActionRequest({
        orgId,
        scope: "internal",
        kind: "user_admin",
        target: "y",
        status: "pending_approval",
        summary: "test",
      });
      expect(bare.actorUserId).toBeNull();
      expect(bare.actorBackend).toBeNull();
    } finally {
      await deleteTestOrg(orgId);
    }
  });

  it("every authenticated broker call lands an audit row with the dual identity", async () => {
    const orgId = uniqueOrgId("sec5b");
    await createTestOrg(orgId);
    try {
      const thread = await createWorkThread(orgId, "t", "web");
      const run = await createWorkRun(orgId, thread.id, "hermes", {
        userId: null,
        role: "admin",
      });
      const broker = await startAgentBroker({
        controlPlane: inProcessControlPlane,
        onEvents: async () => {},
      });
      try {
        const token = broker.tokenFor({ runId: run.id, orgId, kind: "work" });
        const res = await fetch(`http://127.0.0.1:${broker.port}/v1/skills/search`, {
          method: "POST",
          headers: {
            authorization: `Bearer ${token}`,
            "content-type": "application/json",
          },
          body: "{}",
        });
        expect(res.status).toBe(200);
        // The audit write is fire-and-forget; give it a beat.
        await new Promise((r) => setTimeout(r, 200));
        const rows = await db()
          .select()
          .from(control_plane_audit)
          .where(eq(control_plane_audit.org_id, orgId));
        const row = rows.find((r) => r.path === "/v1/skills/search");
        expect(row).toMatchObject({
          run_id: run.id,
          actor_role: "admin",
          backend: "hermes",
        });
      } finally {
        await broker.close();
      }
    } finally {
      await deleteTestOrg(orgId);
    }
  });
});
