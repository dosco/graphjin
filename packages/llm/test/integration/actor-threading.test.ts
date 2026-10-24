import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { app_user, db, eq, pool, work_run, work_thread } from "@neko/db";
import {
  createWorkRun,
  createWorkThread,
  listWorkThreads,
} from "../../src/work/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[actor-threading] skipping: Postgres unreachable.");
}

// K1 — every run carries the acting principal, snapshotted at run start.
describeIfDb("K1 actor threading", () => {
  const orgId = uniqueOrgId("actor");
  let userId: string;
  let adminId: string;

  beforeAll(async () => {
    await createTestOrg(orgId);
    userId = `${orgId}-user`;
    adminId = `${orgId}-admin`;
    await db().insert(app_user).values([
      {
        id: userId,
        email: "ada@example.com",
        org_id: orgId,
      },
      {
        id: adminId,
        email: "admin@example.com",
        org_id: orgId,
      },
    ]);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  it("stamps the web actor (user id + snapshotted role)", async () => {
    const thread = await createWorkThread(orgId, "t", "web", userId);
    expect(thread.created_by_user_id).toBe(userId);

    const run = await createWorkRun(orgId, thread.id, "ax", {
      userId,
      role: "member",
    });
    const [row] = await db()
      .select({
        actorUserId: work_run.actor_user_id,
        actorRole: work_run.actor_role,
      })
      .from(work_run)
      .where(eq(work_run.id, run.id));
    expect(row).toEqual({ actorUserId: userId, actorRole: "member" });
  });

  it("channel + service runs carry role-only identities", async () => {
    const thread = await createWorkThread(orgId, "t2", "telegram");
    expect(thread.created_by_user_id).toBeNull();

    const channelRun = await createWorkRun(orgId, thread.id, "ax", {
      userId: null,
      role: "member",
    });
    const serviceRun = await createWorkRun(orgId, thread.id, "ax", {
      userId: null,
      role: "service",
    });
    const rows = await db()
      .select({
        id: work_run.id,
        actorUserId: work_run.actor_user_id,
        actorRole: work_run.actor_role,
      })
      .from(work_run)
      .where(eq(work_run.thread_id, thread.id));
    const byId = new Map(rows.map((r) => [r.id, r]));
    expect(byId.get(channelRun.id)).toMatchObject({
      actorUserId: null,
      actorRole: "member",
    });
    expect(byId.get(serviceRun.id)).toMatchObject({
      actorUserId: null,
      actorRole: "service",
    });
  });

  it("legacy callers without an actor still create runs (null actor)", async () => {
    const thread = await createWorkThread(orgId);
    const run = await createWorkRun(orgId, thread.id, "ax");
    const [row] = await db()
      .select({ actorRole: work_run.actor_role })
      .from(work_run)
      .where(eq(work_run.id, run.id));
    expect(row?.actorRole).toBeNull();
  });

  it("lists web threads by owner without an admin visibility bypass", async () => {
    const memberThread = await createWorkThread(
      orgId,
      "member private",
      "web",
      userId,
    );
    const adminThread = await createWorkThread(
      orgId,
      "admin private",
      "web",
      adminId,
    );
    const soloThread = await createWorkThread(orgId, "legacy local", "web");

    expect((await listWorkThreads(orgId, "web", userId)).map((row) => row.id))
      .toContain(memberThread.id);
    expect((await listWorkThreads(orgId, "web", userId)).map((row) => row.id))
      .not.toContain(adminThread.id);
    expect((await listWorkThreads(orgId, "web", adminId)).map((row) => row.id))
      .toContain(adminThread.id);
    expect((await listWorkThreads(orgId, "web", adminId)).map((row) => row.id))
      .not.toContain(memberThread.id);
    expect((await listWorkThreads(orgId, "web", null)).map((row) => row.id))
      .toContain(soloThread.id);
  });

  it("separates main history from app-owned history and carries legacy app threads forward", async () => {
    const main = await createWorkThread(orgId, "main", "web", userId);
    const crm = await createWorkThread(orgId, "crm", "web", userId, {
      appContext: { appId: "crm", appLabel: "CRM" },
    });
    const legacyCrm = await createWorkThread(orgId, "legacy crm", "web", userId, {
      recordContext: {
        surface: "list",
        appId: "crm",
        appLabel: "CRM",
        objectApiName: "activity",
        objectLabel: "Activities",
      },
    });
    const support = await createWorkThread(orgId, "support", "web", userId, {
      appContext: { appId: "support", appLabel: "Support" },
    });

    const mainIds = (await listWorkThreads(orgId, "web", userId, {
      surface: "main",
    })).map((thread) => thread.id);
    const crmIds = (await listWorkThreads(orgId, "web", userId, {
      surface: "app",
      appId: "crm",
    })).map((thread) => thread.id);

    expect(mainIds).toContain(main.id);
    expect(mainIds).not.toContain(crm.id);
    expect(mainIds).not.toContain(legacyCrm.id);
    expect(crmIds).toEqual(expect.arrayContaining([crm.id, legacyCrm.id]));
    expect(crmIds).not.toContain(main.id);
    expect(crmIds).not.toContain(support.id);
  });
});
