/**
 * Drizzle CRUD integration test against a real Postgres.
 *
 * Skips automatically when the metadata DB isn't reachable (so it's safe
 * to run in CI environments without a DB; locally `docker compose up -d`
 * makes it run).
 */

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  and,
  asc,
  customer_profile,
  dashboard_pin,
  data_source,
  db,
  desc,
  eq,
  metric,
  metric_snapshot,
  onboarding_wizard,
  organization,
  pool,
  processing_job,
} from "../../src";

const TEST_ORG_ID = `vitest-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;

async function dbReachable(): Promise<boolean> {
  try {
    await pool().query("select 1");
    return true;
  } catch {
    return false;
  }
}

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  // Surface why the suite is skipped — silent skip is a footgun in CI logs.
  console.warn(
    "[drizzle-crud] skipping: metadata Postgres unreachable. Run `docker compose up -d` to enable.",
  );
}

describeIfDb("drizzle CRUD against real Postgres", () => {
  beforeAll(async () => {
    await db().insert(organization).values({
      id: TEST_ORG_ID,
      name: "Vitest Org",
    });
  });

  afterAll(async () => {
    // Cascading deletes via FK on delete cascade clean up child rows.
    await db().delete(organization).where(eq(organization.id, TEST_ORG_ID));
    await pool().end();
  });

  it("organization round-trip", async () => {
    const rows = await db()
      .select({ id: organization.id, name: organization.name })
      .from(organization)
      .where(eq(organization.id, TEST_ORG_ID));
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ id: TEST_ORG_ID, name: "Vitest Org" });
  });

  it("data_source insert / read / update / delete", async () => {
    const ins = await db()
      .insert(data_source)
      .values({
        org_id: TEST_ORG_ID,
        kind: "graphjin",
        graphql_url: "http://example.com/graphql",
        mcp_url: "http://example.com/mcp",
        label: "primary",
      })
      .returning({ id: data_source.id });
    const id = ins[0].id;

    const fetched = await db()
      .select()
      .from(data_source)
      .where(eq(data_source.id, id));
    expect(fetched[0]).toMatchObject({
      org_id: TEST_ORG_ID,
      kind: "graphjin",
      graphql_url: "http://example.com/graphql",
    });

    await db()
      .update(data_source)
      .set({ label: "updated" })
      .where(eq(data_source.id, id));
    const updated = await db().select().from(data_source).where(eq(data_source.id, id));
    expect(updated[0].label).toBe("updated");

    await db().delete(data_source).where(eq(data_source.id, id));
    const after = await db().select().from(data_source).where(eq(data_source.id, id));
    expect(after).toHaveLength(0);
  });

  it("onboarding_wizard preserves text[] arrays", async () => {
    await db().insert(onboarding_wizard).values({
      org_id: TEST_ORG_ID,
      company_note: "test",
      fiscal_year_start_month: 4,
      active_seats: ["CEO", "CFO"],
      priorities: ["growth", "retention"],
      step: "submitted",
    });
    const rows = await db()
      .select()
      .from(onboarding_wizard)
      .where(eq(onboarding_wizard.org_id, TEST_ORG_ID));
    expect(rows[0].active_seats).toEqual(["CEO", "CFO"]);
    expect(rows[0].priorities).toEqual(["growth", "retention"]);
  });

  it("processing_job stores jsonb trigger_payload faithfully", async () => {
    const ins = await db()
      .insert(processing_job)
      .values({
        org_id: TEST_ORG_ID,
        kind: "metric_refresh",
        status: "queued",
        trigger: "test",
        trigger_payload: { metricId: "abc-123", role: "CEO", nested: { a: 1 } },
      })
      .returning({ id: processing_job.id });

    const rows = await db()
      .select({ trigger_payload: processing_job.trigger_payload })
      .from(processing_job)
      .where(eq(processing_job.id, ins[0].id));
    expect(rows[0].trigger_payload).toEqual({
      metricId: "abc-123",
      role: "CEO",
      nested: { a: 1 },
    });
  });

  it("customer_profile partial unique index allows multiple non-current versions", async () => {
    await db().insert(customer_profile).values({
      org_id: TEST_ORG_ID,
      version: 1,
      is_current: false,
      business_profile: "v1",
    });
    await db().insert(customer_profile).values({
      org_id: TEST_ORG_ID,
      version: 2,
      is_current: false,
      business_profile: "v2",
    });
    await db().insert(customer_profile).values({
      org_id: TEST_ORG_ID,
      version: 3,
      is_current: true,
      business_profile: "v3",
    });
    const rows = await db()
      .select({ version: customer_profile.version })
      .from(customer_profile)
      .where(eq(customer_profile.org_id, TEST_ORG_ID))
      .orderBy(asc(customer_profile.version));
    expect(rows.map((r) => r.version)).toEqual([1, 2, 3]);
  });

  it("customer_profile partial unique index blocks two current rows", async () => {
    // The setup from the previous test left version=3 as is_current. Inserting
    // another is_current=true row for the same org must be rejected by the
    // partial unique index `one_current_profile_per_org`.
    await expect(
      db().insert(customer_profile).values({
        org_id: TEST_ORG_ID,
        version: 4,
        is_current: true,
        business_profile: "duplicate-current",
      }),
    ).rejects.toThrow();
  });

  it("dashboard_pin nested relational query loads metric + latest snapshot", async () => {
    const m = await db()
      .insert(metric)
      .values({
        org_id: TEST_ORG_ID,
        role: "CEO",
        slug: "test-revenue",
        source: "bootstrap",
        title: "Revenue MTD",
        chart_hint: "kpi",
      })
      .returning({ id: metric.id });
    const metricId = m[0].id;

    // Two snapshots; query should return the latest.
    await db().insert(metric_snapshot).values({
      metric_id: metricId,
      status: "watch",
      payload: { headlineMetric: "$1M" },
      captured_at: new Date(Date.now() - 60_000),
    });
    await db().insert(metric_snapshot).values({
      metric_id: metricId,
      status: "good",
      payload: { headlineMetric: "$1.2M" },
    });

    await db().insert(dashboard_pin).values({
      org_id: TEST_ORG_ID,
      role: "CEO",
      metric_id: metricId,
      sort_order: 0,
    });

    const pins = await db().query.dashboard_pin.findMany({
      where: and(
        eq(dashboard_pin.org_id, TEST_ORG_ID),
        eq(dashboard_pin.role, "CEO"),
      ),
      with: {
        metric: {
          with: {
            snapshots: {
              orderBy: desc(metric_snapshot.captured_at),
              limit: 1,
            },
          },
        },
      },
    });

    expect(pins).toHaveLength(1);
    expect(pins[0].metric?.title).toBe("Revenue MTD");
    expect(pins[0].metric?.snapshots).toHaveLength(1);
    expect(pins[0].metric?.snapshots[0].status).toBe("good");
    expect((pins[0].metric?.snapshots[0].payload as { headlineMetric?: string })?.headlineMetric)
      .toBe("$1.2M");
  });
});
