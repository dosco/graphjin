import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { channel_workspace, db, eq, pool } from "@neko/db";
import { resolveInboundOrg } from "../../src/channels/delivery.js";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[inbound-org] skipping: Postgres unreachable.");
}

describeIfDb("resolveInboundOrg (CH2)", () => {
  const orgA = uniqueOrgId("ws-a");
  const orgB = uniqueOrgId("ws-b");

  beforeAll(async () => {
    await createTestOrg(orgA);
    await createTestOrg(orgB);
  });

  afterAll(async () => {
    await deleteTestOrg(orgA);
    await deleteTestOrg(orgB);
    await pool().end();
  });

  it("no workspace scope → default org, no row written", async () => {
    expect(await resolveInboundOrg("@x/channel-tg", undefined, orgA)).toBe(orgA);
    const rows = await db()
      .select()
      .from(channel_workspace)
      .where(eq(channel_workspace.org_id, orgA));
    expect(rows).toHaveLength(0);
  });

  it("first contact auto-binds the workspace to the default org", async () => {
    expect(await resolveInboundOrg("@x/channel-slack", "T123", orgA)).toBe(orgA);
    const rows = await db()
      .select()
      .from(channel_workspace)
      .where(eq(channel_workspace.workspace_id, "T123"));
    expect(rows).toHaveLength(1);
    expect(rows[0]!.org_id).toBe(orgA);
  });

  it("a mapped workspace resolves its org regardless of the default", async () => {
    await db()
      .update(channel_workspace)
      .set({ org_id: orgB })
      .where(eq(channel_workspace.workspace_id, "T123"));
    expect(await resolveInboundOrg("@x/channel-slack", "T123", orgA)).toBe(orgB);
  });
});
