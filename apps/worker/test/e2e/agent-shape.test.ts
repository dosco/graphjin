/**
 * E2E shape tests against the running AdventureWorks Docker stack.
 *
 * Self-bootstrapping: the test creates a throwaway org, seeds it with
 * a data_source pointing at the running GraphJin and a primary-provider
 * row, then runs the metric agent for each runnable plan. Drops the org
 * in afterAll.
 *
 * Plan matrix (gated independently by env keys):
 *   - Ax × Google Gemini (GEMINI_API_KEY)
 *   - Ax × Anthropic     (ANTHROPIC_API_KEY)
 *
 * Required external state:
 *   - docker compose stack up (neko-db + adventureworks-db + graphjin)
 *   - At least one of GEMINI_API_KEY / ANTHROPIC_API_KEY set
 *   - graphjin CLI on PATH
 *   - the agent runtime: `openneko serve` and `openneko sandbox`
 *     (pnpm dev:agent), with OPENNEKO_SERVE_SOCKET set
 *
 * Asserts only shape — validateResult returns null, mood + chartType +
 * chartData fields are well-formed. NOT an accuracy gate.
 *
 * Prints a comparison table at the end showing ground-truth vs each
 * plan's headline metric — qualitative observability for whether
 * the agent landed in the right neighbourhood.
 *
 * Run with: pnpm test:e2e
 */

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { randomUUID } from "node:crypto";
import {
  provisionHostConfig,
  runMetricAgent,
  type MetricAgentInput,
} from "@neko/llm";
import { validateResult } from "../../src/jobs/metric-refresh.js";
import { detectRunnablePlans, type RunPlan } from "./_can-run";
import { seedE2ETestOrg } from "./_seed-org";
import { applyPlan } from "./_set-backend";
import { pickGroundTruth } from "./_ground-truth";
import { formatDeltaPct, parseHeadline } from "./_parse-headline";
import type { TimeWindowGrain } from "@neko/llm";

type SampleCard = Pick<MetricAgentInput, "role" | "slug" | "title" | "why" | "chartHint">;

const SAMPLE_CARDS: SampleCard[] = [
  {
    role: "CEO",
    slug: "revenue-by-channel",
    title: "Revenue by sales channel",
    why: "Quick read on where revenue is coming from",
    chartHint: "donut",
  },
  {
    role: "CFO",
    slug: "order-volume-trend",
    title: "Total order volume",
    why: "How many orders we've taken overall",
    chartHint: "kpi",
  },
];

type RunRecord = {
  planId: string;
  slug: string;
  durationMs: number;
  headline: string;
  parsed: number | null;
  grain: TimeWindowGrain | undefined;
  windowLabel: string;
};

// Module-level so beforeAll/afterAll across describe.each scopes share it.
const runs: RunRecord[] = [];

const detection = await detectRunnablePlans();

if (detection.blockingReasons.length > 0) {
  for (const reason of detection.blockingReasons) {
    console.warn(`[e2e] BLOCKING: ${reason}`);
  }
  console.warn("[e2e] suite skipped — see warnings above");
}

const describeIfRunnable =
  detection.blockingReasons.length === 0 ? describe : describe.skip;
const runnable: RunPlan[] = detection.runnable;

describeIfRunnable("E2E: metric agent shape", () => {
  let testOrgId: string;
  let cleanup: () => Promise<void>;

  beforeAll(async () => {
    if (runnable.length === 0) return;
    const seeded = await seedE2ETestOrg({
      graphqlUrl: detection.graphqlUrl,
      initialPlan: runnable[0],
    });
    testOrgId = seeded.orgId;
    cleanup = seeded.cleanup;
    console.log(`[e2e] seeded throwaway org id=${testOrgId}`);
  });

  afterAll(async () => {
    if (cleanup) {
      await cleanup();
      console.log(`[e2e] dropped throwaway org id=${testOrgId}`);
    }
    printComparisonTable(runs, runnable);
  });

  if (runnable.length === 0) {
    it("no runnable plans — nothing to verify", () => {
      console.warn("[e2e] no plans runnable on this host");
    });
    return;
  }

  describe.each(runnable)("plan=$id", (plan) => {
    beforeAll(async () => {
      await applyPlan(testOrgId, plan);
      // Re-provision so the agent sandbox picks up the selected provider.
      await provisionHostConfig(testOrgId);
    });

    describe.each(SAMPLE_CARDS)("card $slug", (card) => {
      it("produces a well-shaped snapshot", async () => {
        const start = Date.now();
        const result = await runMetricAgent({
          orgId: testOrgId,
          // Production metric refreshes always carry a UUID processing-job ID.
          // Mirror that here so audit writes exercise their real FK/UUID path.
          jobId: randomUUID(),
          role: card.role,
          slug: card.slug,
          title: card.title,
          why: card.why,
          chartHint: card.chartHint,
          debug: false,
        });
        const durationMs = Date.now() - start;

        runs.push({
          planId: plan.id,
          slug: card.slug,
          durationMs,
          headline: result.headlineMetric,
          parsed: parseHeadline(result.headlineMetric),
          grain: result.timeWindow?.grain,
          windowLabel: result.timeWindow?.label ?? "",
        });

        const validationError = validateResult(result);
        expect(
          validationError,
          `validateResult error: ${validationError}`,
        ).toBeNull();

        expect(["good", "watch", "bad"]).toContain(result.mood);
        expect(["kpi", "line", "bar", "donut", "area"]).toContain(
          result.chartType,
        );
        expect(result.chartData.length).toBeGreaterThan(0);
        for (const point of result.chartData) {
          expect(typeof point.v).toBe("number");
          expect(Number.isNaN(point.v)).toBe(false);
          expect(point.d.length).toBeGreaterThan(0);
        }
        expect(result.headlineMetric.length).toBeGreaterThan(0);
        expect(result.headlineLabel.length).toBeGreaterThan(0);
      });
    });
  });
});

// ────────────────────────────────────────────────────────────────────
// Comparison table
// ────────────────────────────────────────────────────────────────────

function planCellFor(planId: string, slug: string, rs: RunRecord[]): string {
  const r = rs.find((x) => x.planId === planId && x.slug === slug);
  if (!r) return "—";
  const dur = `${(r.durationMs / 1000).toFixed(0)}s`;
  const window = r.windowLabel ? ` · ${r.windowLabel}` : "";
  const { gt } = pickGroundTruth(slug, r.grain);
  if (!gt) return `${r.headline}${window} (${dur})`;
  const delta = formatDeltaPct(r.parsed, gt.value);
  return `${r.headline}${window} (${delta} vs ${gt.display}, ${dur})`;
}

function printComparisonTable(rs: RunRecord[], plans: RunPlan[]): void {
  if (rs.length === 0) return;

  const slugs = Array.from(new Set(rs.map((r) => r.slug)));

  const cardWidth = Math.max("Card".length, ...slugs.map((s) => s.length));
  const planCols = plans.map((p) => {
    const cells = slugs.map((s) => planCellFor(p.id, s, rs));
    return {
      planId: p.id,
      width: Math.max(p.id.length, ...cells.map((c) => c.length)),
      cells,
    };
  });

  const pad = (s: string, w: number) => s + " ".repeat(Math.max(0, w - s.length));

  const header = [
    pad("Card", cardWidth),
    ...planCols.map((c) => pad(c.planId, c.width)),
  ].join("  ");
  const separator = "─".repeat(header.length);

  console.log("");
  console.log(
    "E2E results (shape passed; values shown vs grain-matched ground truth)",
  );
  console.log(separator);
  console.log(header);
  console.log(separator);
  for (let i = 0; i < slugs.length; i++) {
    const slug = slugs[i];
    const row = [
      pad(slug, cardWidth),
      ...planCols.map((c) => pad(c.cells[i], c.width)),
    ].join("  ");
    console.log(row);
  }
  console.log(separator);
  console.log("");
}
