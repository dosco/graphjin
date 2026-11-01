import { afterAll, describe, expect, it } from "vitest";
import { db, eq, pool, work_thread } from "@neko/db";
import { dbReachable, withTestOrg } from "@neko/db/test-helpers";
import { channelThreadId, getOrCreateChannelThread } from "../../src/work/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[channel-thread-store] skipping: metadata Postgres unreachable.");
}

describeIfDb("getOrCreateChannelThread (upsert round-trip)", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("creates once, then reuses the same row across turns", async () => {
    await withTestOrg(async (orgId) => {
      const first = await getOrCreateChannelThread({
        orgId,
        channel: "slack",
        conversationKey: "D9",
        title: "Slack DM",
      });
      const second = await getOrCreateChannelThread({
        orgId,
        channel: "slack",
        conversationKey: "D9",
        title: "ignored on reuse",
      });

      expect(first.id).toBe(channelThreadId(orgId, "slack", "D9"));
      expect(second.id).toBe(first.id);

      const rows = await db()
        .select()
        .from(work_thread)
        .where(eq(work_thread.id, first.id));
      expect(rows).toHaveLength(1);
      // Reuse must not clobber the thread created on first contact.
      expect(rows[0].title).toBe("Slack DM");
    });
  });

  it("keys distinct conversations (DM vs thread) to distinct rows", async () => {
    await withTestOrg(async (orgId) => {
      const dm = await getOrCreateChannelThread({
        orgId,
        channel: "slack",
        conversationKey: "D9",
      });
      const thread = await getOrCreateChannelThread({
        orgId,
        channel: "slack",
        conversationKey: "C9:171.5",
      });

      expect(thread.id).not.toBe(dm.id);
      const all = await db()
        .select({ id: work_thread.id })
        .from(work_thread)
        .where(eq(work_thread.org_id, orgId));
      expect(all).toHaveLength(2);
    });
  });
});
