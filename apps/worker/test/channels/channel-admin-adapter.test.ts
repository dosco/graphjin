// ADM5 — the channel_admin action adapter executes the same verbs as
// the admin-map API once an admin approves the chat-proposed change.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { and, app_user, channel_identity, db, eq, pool } from "@neko/db";
import {
  createActionRequest,
  executeApprovedActionRequest,
} from "@neko/llm/workflows";
import { inProcessControlPlane } from "@neko/llm/work";
import { registerChannelAdminAdapter } from "../../src/admin-actions/adapters.js";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[channel-admin-adapter] skipping: Postgres unreachable.");
}

describeIfDb("channel_admin adapter (ADM5)", () => {
  const orgId = uniqueOrgId("chadm");
  let ada: string;
  let identityId: string;

  beforeAll(async () => {
    registerChannelAdminAdapter();
    await createTestOrg(orgId);
    ada = `${orgId}-ada`;
    await db().insert(app_user).values({
      id: ada,
      email: "ada@example.com",
      org_id: orgId,
    });
    const [identity] = await db()
      .insert(channel_identity)
      .values({
        org_id: orgId,
        channel_plugin: "@open-neko/channel-telegram",
        channel_user_id: "12345",
        display_name: "Ada",
      })
      .returning({ id: channel_identity.id });
    identityId = identity.id;
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  async function runAction(payload: Record<string, unknown>) {
    const request = await createActionRequest({
      orgId,
      scope: "internal",
      kind: "channel_admin",
      target: identityId,
      payload,
      riskLevel: "high",
      status: "approved",
      summary: "test",
    });
    return executeApprovedActionRequest(orgId, request.id);
  }

  async function identityRow() {
    const [row] = await db()
      .select()
      .from(channel_identity)
      .where(
        and(
          eq(channel_identity.org_id, orgId),
          eq(channel_identity.id, identityId),
        ),
      );
    return row;
  }

  it("link binds the identity to an active app_user", async () => {
    const result = await runAction({
      action: "link",
      identityId,
      appUserId: ada,
    });
    expect(result.ok).toBe(true);
    const row = await identityRow();
    expect(row).toMatchObject({ status: "linked", app_user_id: ada });
    expect(row.verified_at).toBeTruthy();
  });

  it("block and unblock flip the status", async () => {
    expect((await runAction({ action: "block", identityId })).ok).toBe(true);
    expect((await identityRow()).status).toBe("blocked");
    expect((await runAction({ action: "unblock", identityId })).ok).toBe(true);
    const row = await identityRow();
    expect(row).toMatchObject({ status: "unverified", app_user_id: null });
  });

  it("link to an unknown user fails the request", async () => {
    const result = await runAction({
      action: "link",
      identityId,
      appUserId: "nope",
    });
    expect(result.ok).toBe(false);
    expect(result.error).toMatch(/unknown or disabled/i);
  });

  it("listChannels surfaces workspaces + identities for the agent", async () => {
    const channels = await inProcessControlPlane.listChannels({ orgId });
    expect(channels.identities.some((i) => i.id === identityId)).toBe(true);
  });
});
