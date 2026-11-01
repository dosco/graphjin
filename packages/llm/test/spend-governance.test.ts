import { describe, expect, it } from "vitest";
import { pool } from "@neko/db";
import {
  acknowledgeSpendAlert,
  admitRunSpend,
  createRunSpendGuard,
  listOpenSpendAlerts,
  loadMetricRefreshSettings,
  SpendCapExceeded,
  spendCapFromSignal,
  committedSpendMicros,
  priceUsage,
  recordUsageSpend,
  releaseSpendReservation,
  reserveSpend,
  getSpendSettings,
  saveSpendLimits,
  saveWorkflowSpendOverride,
  SpendBudgetExceeded,
  SpendSettingsError,
  spendWindows,
} from "../src/spend";
import { appendWorkRunEvent, createWorkRun, createWorkThread, finishWorkRun } from "../src/work/store";

async function dbReachable(): Promise<boolean> {
  try {
    await pool().query("select 1 from spend_limit limit 1");
    return true;
  } catch {
    return false;
  }
}

const describeIfDb = (await dbReachable()) ? describe : describe.skip;

async function withOrg<T>(fn: (orgId: string) => Promise<T>): Promise<T> {
  const orgId = `spend-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
  await pool().query("insert into organization (id, name) values ($1, 'Spend test')", [orgId]);
  try {
    return await fn(orgId);
  } finally {
    await pool().query("delete from organization where id = $1", [orgId]);
  }
}

async function setOrgLimits(orgId: string, usd: Partial<Record<string, number>>) {
  for (const [column, value] of Object.entries(usd)) {
    await pool().query(`update spend_limit set ${column} = $2 where org_id = $1 and workflow_id is null`, [
      orgId,
      Math.round((value ?? 0) * 1_000_000),
    ]);
  }
}

async function run(orgId: string, spend?: Parameters<typeof createWorkRun>[4]) {
  const thread = await createWorkThread(orgId, "spend", "web");
  return createWorkRun(orgId, thread.id, "ax", undefined, spend);
}

async function workflow(orgId: string): Promise<string> {
  const { rows } = await pool().query<{ id: string }>(
    "insert into workflow_definition (org_id, name) values ($1, $2) returning id",
    [orgId, `wf-${Math.random().toString(36).slice(2, 8)}`],
  );
  return rows[0]!.id;
}

const usage = (extra: Record<string, unknown>) => ({
  inputTokens: 1_000,
  outputTokens: 100,
  totalTokens: 1_100,
  coverage: "complete" as const,
  ...extra,
});

describe("priceUsage", () => {
  it("prefers billed cost, then the estimate, and never prices an unknown turn at zero", () => {
    expect(priceUsage(usage({ billedCostUsd: 0.02, estimatedCostUsd: 0.5 }), 5_000_000)).toEqual({
      costMicros: 20_000,
      priced: "billed",
    });
    expect(priceUsage(usage({ estimatedCostUsd: 0.4267, costStatus: "estimated" }), 5_000_000)).toEqual({
      costMicros: 426_700,
      priced: "estimated",
    });
    expect(priceUsage(usage({ estimatedCostUsd: 0, costStatus: "included" }), 5_000_000)).toEqual({
      costMicros: 0,
      priced: "included",
    });
    expect(priceUsage(usage({ estimatedCostUsd: 0.001, costStatus: "unknown" }), 5_000_000)).toEqual({
      costMicros: 5_000_000,
      priced: "fallback",
    });
    expect(priceUsage({ coverage: "unavailable" }, 7_000_000)).toEqual({ costMicros: 7_000_000, priced: "fallback" });
  });

  it("uses UTC calendar windows", () => {
    const w = spendWindows(new Date("2026-09-17T09:42:10Z"));
    expect(w.hourStart.toISOString()).toBe("2026-09-17T09:00:00.000Z");
    expect(w.dayEnd.toISOString()).toBe("2026-09-18T00:00:00.000Z");
  });
});

describeIfDb("spend admission", () => {
  it("seeds default limits for a new organization", async () => {
    await withOrg(async (orgId) => {
      const { rows } = await pool().query(
        "select run_cap_micros, org_hourly_micros, org_daily_micros, workflow_hourly_micros, workflow_daily_micros, warn_percent from spend_limit where org_id = $1",
        [orgId],
      );
      expect(rows).toEqual([
        {
          run_cap_micros: "5000000",
          org_hourly_micros: "200000000",
          org_daily_micros: "500000000",
          workflow_hourly_micros: "50000000",
          workflow_daily_micros: "200000000",
          warn_percent: 80,
        },
      ]);
    });
  });

  it("reserves the run cap with the run and rolls the run back when a budget is full", async () => {
    await withOrg(async (orgId) => {
      await setOrgLimits(orgId, { org_hourly_micros: 12 });
      const first = await run(orgId, { source: "chat" });
      await run(orgId, { source: "chat" });
      const reservation = await pool().query(
        "select source, reserved_micros from spend_reservation where work_run_id = $1",
        [first.id],
      );
      expect(reservation.rows).toEqual([{ source: "chat", reserved_micros: "5000000" }]);

      const blocked = await run(orgId, { source: "chat" }).catch((error) => error);
      expect(blocked).toBeInstanceOf(SpendBudgetExceeded);
      expect(blocked).toMatchObject({ budget: "org_hourly", limitUsd: 12, committedUsd: 10, reservationUsd: 5 });
      const runs = await pool().query("select count(*)::int as n from work_run where org_id = $1", [orgId]);
      expect(runs.rows[0].n).toBe(2);
    });
  });

  it("admits at equality with the limit", async () => {
    await withOrg(async (orgId) => {
      await setOrgLimits(orgId, { org_hourly_micros: 10 });
      await run(orgId);
      await expect(run(orgId)).resolves.toBeTruthy();
      await expect(run(orgId)).rejects.toBeInstanceOf(SpendBudgetExceeded);
    });
  });

  it("releases a reservation when its run ends and counts only real spend", async () => {
    await withOrg(async (orgId) => {
      const r = await run(orgId, { source: "chat" });
      await appendWorkRunEvent({
        orgId,
        threadId: r.thread_id,
        runId: r.id,
        event: { type: "usage", source: "outer", provider: "gemini", model: "gemini-3.7-flash", usage: usage({ estimatedCostUsd: 1.25, costStatus: "estimated", costSource: "official_docs_snapshot", pricingCatalogVersion: "google-pricing-2026-09-17" }) },
      });
      const since = spendWindows(new Date()).dayStart;
      expect(await committedSpendMicros(pool(), { orgId, since })).toBe(5_000_000);
      await finishWorkRun(r.id, "completed", null);
      expect(await committedSpendMicros(pool(), { orgId, since })).toBe(1_250_000);
      const ledger = await pool().query(
        "select source, provider, model, cost_micros, tokens, priced, cost_source, pricing_version from spend_ledger where work_run_id = $1",
        [r.id],
      );
      expect(ledger.rows).toEqual([
        {
          source: "chat",
          provider: "gemini",
          model: "gemini-3.7-flash",
          cost_micros: "1250000",
          tokens: "1100",
          priced: "estimated",
          cost_source: "official_docs_snapshot",
          pricing_version: "google-pricing-2026-09-17",
        },
      ]);
    });
  });

  it("charges the run cap for an unpriced turn", async () => {
    await withOrg(async (orgId) => {
      const r = await run(orgId);
      await appendWorkRunEvent({
        orgId,
        threadId: r.thread_id,
        runId: r.id,
        event: { type: "usage", source: "outer", usage: usage({ costStatus: "unknown" }) },
      });
      const ledger = await pool().query("select cost_micros, priced from spend_ledger where work_run_id = $1", [r.id]);
      expect(ledger.rows).toEqual([{ cost_micros: "5000000", priced: "fallback" }]);
    });
  });

  it("keeps counting an open reservation after its window has passed", async () => {
    await withOrg(async (orgId) => {
      const r = await run(orgId);
      await pool().query("update spend_reservation set created_at = now() - interval '3 hours' where work_run_id = $1", [r.id]);
      expect(await committedSpendMicros(pool(), { orgId, since: spendWindows(new Date()).hourStart })).toBe(5_000_000);
    });
  });

  it("admits exactly as many parallel runs as the budget holds", async () => {
    await withOrg(async (orgId) => {
      await setOrgLimits(orgId, { org_hourly_micros: 15 });
      const results = await Promise.allSettled(Array.from({ length: 20 }, () => run(orgId)));
      expect(results.filter((r) => r.status === "fulfilled")).toHaveLength(3);
      expect(
        results.filter((r) => r.status === "rejected" && r.reason instanceof SpendBudgetExceeded),
      ).toHaveLength(17);
    });
  });

  it("applies workflow budgets, with a workflow override", async () => {
    await withOrg(async (orgId) => {
      const wf = await workflow(orgId);
      const other = await workflow(orgId);
      await pool().query(
        "insert into spend_limit (org_id, workflow_id, workflow_hourly_micros) values ($1, $2, 6000000)",
        [orgId, wf],
      );
      await run(orgId, { source: "cron", workflowId: wf });
      const blocked = await run(orgId, { source: "cron", workflowId: wf }).catch((error) => error);
      expect(blocked).toMatchObject({ budget: "workflow_hourly", limitUsd: 6 });
      await expect(run(orgId, { source: "cron", workflowId: other })).resolves.toBeTruthy();
    });
  });

  it("reserves and releases spend outside a work run", async () => {
    await withOrg(async (orgId) => {
      const admission = await reserveSpend({ orgId, source: "metric" });
      const since = spendWindows(new Date()).dayStart;
      expect(await committedSpendMicros(pool(), { orgId, since })).toBe(5_000_000);
      await recordUsageSpend({ orgId, reservationId: admission.reservationId, usage: usage({ billedCostUsd: 0.3 }) });
      await releaseSpendReservation(admission.reservationId);
      expect(await committedSpendMicros(pool(), { orgId, since })).toBe(300_000);
      const ledger = await pool().query("select source, priced from spend_ledger where org_id = $1", [orgId]);
      expect(ledger.rows).toEqual([{ source: "metric", priced: "billed" }]);
    });
  });

  it("fails closed when an organization has no limits", async () => {
    await withOrg(async (orgId) => {
      await pool().query("delete from spend_limit where org_id = $1", [orgId]);
      await expect(run(orgId)).rejects.toThrow(/No spend limits/);
    });
  });

  it("clamps a limit above the operator ceiling", async () => {
    await withOrg(async (orgId) => {
      await setOrgLimits(orgId, { run_cap_micros: 900 });
      const client = await pool().connect();
      try {
        await client.query("begin");
        const admission = await admitRunSpend(client, { orgId, source: "system" });
        await client.query("rollback");
        expect(admission.reservedMicros).toBe(50_000_000);
      } finally {
        client.release();
      }
    });
  });
});

describeIfDb("spend settings", () => {
  const valid = {
    runCapUsd: 4,
    orgHourlyUsd: 150,
    orgDailyUsd: 400,
    workflowHourlyUsd: 40,
    workflowDailyUsd: 120,
    warnPercent: 75,
  };

  it("reports limits, ceilings, window usage and workflow rows", async () => {
    await withOrg(async (orgId) => {
      const wf = await workflow(orgId);
      const r = await run(orgId, { source: "cron", workflowId: wf });
      await appendWorkRunEvent({
        orgId,
        threadId: r.thread_id,
        runId: r.id,
        event: { type: "usage", source: "outer", usage: usage({ estimatedCostUsd: 0.75, costStatus: "estimated" }) },
      });
      const settings = await getSpendSettings(orgId);
      expect(settings.limits).toEqual({
        runCapUsd: 5,
        orgHourlyUsd: 200,
        orgDailyUsd: 500,
        workflowHourlyUsd: 50,
        workflowDailyUsd: 200,
        warnPercent: 80,
      });
      expect(settings.ceilings).toEqual({
        runCapUsd: 50,
        orgHourlyUsd: 500,
        orgDailyUsd: 2000,
        workflowHourlyUsd: 100,
        workflowDailyUsd: 500,
      });
      expect(settings.org.day).toMatchObject({ spentUsd: 0.75, heldUsd: 4.25, limitUsd: 500 });
      expect(settings.workflows).toEqual([
        expect.objectContaining({ workflowId: wf, hourlyOverrideUsd: null, hourlyLimitUsd: 50, spentTodayUsd: 0.75 }),
      ]);
    });
  });

  it("saves valid limits and applies them to the next admission", async () => {
    await withOrg(async (orgId) => {
      const saved = await saveSpendLimits(orgId, null, valid);
      expect(saved.limits).toEqual(valid);
      const r = await run(orgId);
      const reservation = await pool().query("select reserved_micros from spend_reservation where work_run_id = $1", [r.id]);
      expect(reservation.rows[0].reserved_micros).toBe("4000000");
    });
  });

  it("saves the metric refresh setting and keeps it when a save omits it", async () => {
    await withOrg(async (orgId) => {
      expect((await getSpendSettings(orgId)).metricRefresh).toBe("card");
      expect((await saveSpendLimits(orgId, null, { ...valid, metricRefresh: "weekly" })).metricRefresh).toBe("weekly");
      expect((await saveSpendLimits(orgId, null, valid)).metricRefresh).toBe("weekly");
      expect((await loadMetricRefreshSettings(pool())).get(orgId)).toBe("weekly");
      expect((await saveSpendLimits(orgId, null, { ...valid, metricRefresh: "card" })).metricRefresh).toBe("card");
      await expect(
        saveSpendLimits(orgId, null, { ...valid, metricRefresh: "hourly" as never }),
      ).rejects.toThrow("Metric refresh must be card, daily, weekly or off.");
    });
  });

  it("rejects invalid limits with a message that names the field", async () => {
    await withOrg(async (orgId) => {
      await expect(saveSpendLimits(orgId, null, { ...valid, orgDailyUsd: 2500 })).rejects.toThrow(
        "Organization daily budget cannot exceed $2000.00.",
      );
      await expect(saveSpendLimits(orgId, null, { ...valid, runCapUsd: 0 })).rejects.toThrow(
        "Per-run cap must be a positive dollar amount.",
      );
      await expect(saveSpendLimits(orgId, null, { ...valid, orgHourlyUsd: 450 })).rejects.toThrow(
        "Organization hourly budget cannot exceed the daily budget.",
      );
      await expect(saveSpendLimits(orgId, null, { ...valid, runCapUsd: 45 })).rejects.toThrow(
        "Per-run cap cannot exceed an hourly budget",
      );
      await expect(saveSpendLimits(orgId, null, { ...valid, warnPercent: 30 })).rejects.toBeInstanceOf(SpendSettingsError);
      expect((await getSpendSettings(orgId)).limits.runCapUsd).toBe(5);
    });
  });

  it("sets and clears a workflow override", async () => {
    await withOrg(async (orgId) => {
      const wf = await workflow(orgId);
      let settings = await saveWorkflowSpendOverride(orgId, null, wf, { hourlyUsd: 6, dailyUsd: null });
      expect(settings.workflows[0]).toMatchObject({ hourlyOverrideUsd: 6, hourlyLimitUsd: 6, dailyOverrideUsd: null, dailyLimitUsd: 200 });
      await run(orgId, { source: "cron", workflowId: wf });
      await expect(run(orgId, { source: "cron", workflowId: wf })).rejects.toMatchObject({ budget: "workflow_hourly" });
      settings = await saveWorkflowSpendOverride(orgId, null, wf, { hourlyUsd: null, dailyUsd: null });
      expect(settings.workflows[0]).toMatchObject({ hourlyOverrideUsd: null, hourlyLimitUsd: 50 });
      await expect(
        saveWorkflowSpendOverride(orgId, null, "00000000-0000-0000-0000-000000000000", { hourlyUsd: 1, dailyUsd: 2 }),
      ).rejects.toThrow("Workflow not found.");
    });
  });
});

describeIfDb("run spend guard", () => {
  it("stops at a tool call once the running turn passes the cap", async () => {
    await withOrg(async (orgId) => {
      const r = await run(orgId);
      const events: Array<{ type: string; message?: string }> = [];
      const guard = await createRunSpendGuard({ runId: r.id, emit: async (event) => void events.push(event as { type: string }) });
      await guard.emit({ type: "tool_start", id: "a", name: "execute", input: {}, usageSnapshot: usage({ estimatedCostUsd: 4.9, costStatus: "estimated" }) });
      expect(guard.signal.aborted).toBe(false);
      await guard.emit({ type: "tool_start", id: "b", name: "execute", input: {}, usageSnapshot: usage({ estimatedCostUsd: 5.2, costStatus: "estimated" }) });
      expect(guard.signal.aborted).toBe(true);
      expect(spendCapFromSignal(guard.signal)).toBeInstanceOf(SpendCapExceeded);
      expect(events.map((e) => e.type)).toEqual(["tool_start", "tool_start", "error"]);
      const row = await pool().query("select status, error from work_run where id = $1", [r.id]);
      expect(row.rows[0]).toEqual({ status: "failed", error: "The run exceeded its $5.00 spend cap ($5.20 spent)." });
    });
  });

  it("adds completed turns and ignores a snapshot without a price", async () => {
    await withOrg(async (orgId) => {
      const r = await run(orgId);
      const guard = await createRunSpendGuard({ runId: r.id, emit: async () => {} });
      await guard.emit({ type: "usage", source: "outer", usage: usage({ estimatedCostUsd: 3, costStatus: "estimated" }) });
      await guard.emit({ type: "tool_start", id: "a", name: "execute", input: {}, usageSnapshot: usage({}) });
      expect(guard.signal.aborted).toBe(false);
      await guard.emit({ type: "tool_start", id: "b", name: "execute", input: {}, usageSnapshot: usage({ estimatedCostUsd: 2.5, costStatus: "estimated" }) });
      expect(guard.exceeded()?.message).toBe("The run exceeded its $5.00 spend cap ($5.50 spent).");
    });
  });

  it("forwards a parent cancel without marking a spend stop", async () => {
    await withOrg(async (orgId) => {
      const r = await run(orgId);
      const parent = new AbortController();
      const guard = await createRunSpendGuard({ runId: r.id, emit: async () => {}, signal: parent.signal });
      parent.abort();
      expect(guard.signal.aborted).toBe(true);
      expect(spendCapFromSignal(guard.signal)).toBeNull();
      guard.dispose();
    });
  });
});

describeIfDb("spend alerts", () => {
  const alerts = (orgId: string) =>
    pool().query<{ kind: string; subject: string; message: string }>(
      "select kind, subject, details->>'message' as message from behavior_alert where org_id = $1 order by kind, subject",
      [orgId],
    );

  it("warns once per window when spend reaches the warning share", async () => {
    await withOrg(async (orgId) => {
      await setOrgLimits(orgId, { org_hourly_micros: 20, org_daily_micros: 500 });
      const r = await run(orgId);
      const event = { type: "usage" as const, source: "outer" as const, usage: usage({ estimatedCostUsd: 12, costStatus: "estimated" }) };
      await appendWorkRunEvent({ orgId, threadId: r.thread_id, runId: r.id, event });
      await appendWorkRunEvent({ orgId, threadId: r.thread_id, runId: r.id, event });
      const { rows } = await alerts(orgId);
      expect(rows).toHaveLength(1);
      expect(rows[0]).toMatchObject({ kind: "spend.budget_warning", subject: "org" });
      expect(rows[0]!.message).toMatch(/^Spend is at 120% of the \$20\.00 hourly budget for this organization \(\$24\.00 so far\)\./);
      expect((await listOpenSpendAlerts(orgId)).map((a) => a.kind)).toEqual(["spend.budget_warning"]);
    });
  });

  it("records a blocked run and an unknown price, and lets an admin acknowledge an alert", async () => {
    await withOrg(async (orgId) => {
      await setOrgLimits(orgId, { org_hourly_micros: 6 });
      const r = await run(orgId);
      await expect(run(orgId)).rejects.toBeInstanceOf(SpendBudgetExceeded);
      await expect(run(orgId)).rejects.toBeInstanceOf(SpendBudgetExceeded);
      await appendWorkRunEvent({
        orgId,
        threadId: r.thread_id,
        runId: r.id,
        event: { type: "usage", source: "outer", provider: "gemini", model: "gemini-9-flash", usage: usage({ costStatus: "unknown" }) },
      });
      const { rows } = await alerts(orgId);
      expect(rows.map((row) => [row.kind, row.subject])).toEqual([
        ["spend.budget_blocked", "org"],
        ["spend.budget_warning", "org"],
        ["spend.price_unknown", "model:gemini/gemini-9-flash"],
      ]);
      const open = await listOpenSpendAlerts(orgId);
      expect(await acknowledgeSpendAlert(orgId, open[0]!.id, null)).toBe(true);
      expect(await acknowledgeSpendAlert(orgId, open[0]!.id, null)).toBe(false);
      expect(await listOpenSpendAlerts(orgId)).toHaveLength(2);
    });
  });
});
