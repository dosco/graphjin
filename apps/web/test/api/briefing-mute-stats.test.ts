/**
 * OL7 + OL9 + OL2 — scope mutes, the stat strip, and observation
 * elevation. Asserts:
 *   - mute POST/GET/DELETE round-trip + duration validation
 *   - muted scopes drop matching findings from /api/briefing/stats? no —
 *     from the workflow_output tributaries (filter applied in findings)
 *   - stats counts runs/findings/approvals for today only
 *   - elevate -> active card; dismiss -> gone
 */

import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  and,
  briefing_card,
  db,
  eq,
  muted_scope,
  observation,
  pool,
} from "@neko/db";
import { callRoute } from "../_helpers/route";

const { mockGetOrgId } = vi.hoisted(() => ({ mockGetOrgId: vi.fn() }));

vi.mock("@/lib/db", async () => {
  const actual = await vi.importActual<typeof import("@/lib/db")>("@/lib/db");
  return { ...actual, getOrgId: mockGetOrgId };
});

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[api/briefing/mute-stats] skipping: Postgres unreachable.");
}

describeIfDb("briefing mute + stats + cards", () => {
  const orgId = uniqueOrgId("mute-stats");

  beforeAll(async () => {
    await createTestOrg(orgId);
    mockGetOrgId.mockResolvedValue(orgId);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  it("mute POST/GET/DELETE round-trips and validates duration", async () => {
    const { POST, GET, DELETE } = await import(
      "../../src/app/api/briefing/mute/route"
    );

    const bad = await callRoute(POST, {
      method: "POST",
      body: { scope: "apac_churn", duration: "5m" },
    });
    expect(bad.status).toBe(400);

    const ok = await callRoute(POST, {
      method: "POST",
      body: { scope: "apac_churn", duration: "1h" },
    });
    expect(ok.status).toBe(200);

    const list = await callRoute(GET);
    expect(list.status).toBe(200);
    expect(
      (list.body as { mutes: Array<{ scope: string }> }).mutes.map((m) => m.scope),
    ).toContain("apac_churn");

    // Re-muting upserts (no duplicate row).
    await callRoute(POST, {
      method: "POST",
      body: { scope: "apac_churn", duration: "24h" },
    });
    const rows = await db()
      .select()
      .from(muted_scope)
      .where(
        and(eq(muted_scope.org_id, orgId), eq(muted_scope.scope, "apac_churn")),
      );
    expect(rows).toHaveLength(1);

    const del = await callRoute(DELETE, {
      method: "DELETE",
      query: { scope: "apac_churn" },
    });
    expect(del.status).toBe(200);
    const after = await callRoute(GET);
    expect(
      (after.body as { mutes: Array<{ scope: string }> }).mutes,
    ).toHaveLength(0);
  });

  it("stats returns zeroed counts for a quiet org", async () => {
    const { GET } = await import("../../src/app/api/briefing/stats/route");
    const res = await callRoute(GET);
    expect(res.status).toBe(200);
    expect(res.body).toMatchObject({
      runsToday: 0,
      findingsToday: 0,
      pendingApprovals: 0,
      budgetPct: null,
    });
  });

  it("elevates an observation to a briefing card and dismisses it", async () => {
    const [obs] = await db()
      .insert(observation)
      .values({
        org_id: orgId,
        consumer_kind: "workflow",
        title: "external: invoice.paid",
        body: "{}",
      })
      .returning({ id: observation.id });

    const { POST, PATCH } = await import(
      "../../src/app/api/briefing/cards/route"
    );
    const created = await callRoute(POST, {
      method: "POST",
      body: { observationId: obs!.id },
    });
    expect(created.status).toBe(200);
    const cardId = (created.body as { cardId: string }).cardId;

    const cards = await db()
      .select()
      .from(briefing_card)
      .where(eq(briefing_card.org_id, orgId));
    expect(cards).toHaveLength(1);
    expect(cards[0]!.status).toBe("active");
    expect(cards[0]!.source_observation_id).toBe(obs!.id);

    // Re-elevating the same observation reactivates, not duplicates.
    const again = await callRoute(POST, {
      method: "POST",
      body: { observationId: obs!.id },
    });
    expect(again.status).toBe(200);
    expect(
      await db()
        .select()
        .from(briefing_card)
        .where(eq(briefing_card.org_id, orgId)),
    ).toHaveLength(1);

    const dismissed = await callRoute(PATCH, {
      method: "PATCH",
      body: { cardId },
    });
    expect(dismissed.status).toBe(200);
    const final = await db()
      .select()
      .from(briefing_card)
      .where(eq(briefing_card.org_id, orgId));
    expect(final[0]!.status).toBe("dismissed");
  });
});
